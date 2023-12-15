package internal

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/flynn/go-shlex"
	"github.com/kr/pty"
	"golang.org/x/crypto/ssh"
)

type exitStatusMsg struct {
	Status uint32
}

func exitStatus(err error) (exitStatusMsg, error) {
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// There is no platform independent way to retrieve
			// the exit code, but the following will work on Unix
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return exitStatusMsg{uint32(status.ExitStatus())}, nil
			}
		}
		return exitStatusMsg{0}, err
	}
	return exitStatusMsg{0}, nil
}

func handlerCmd(handler string, appendArgs ...string) (*exec.Cmd, error) {
	handlerSplit, err := shlex.Split(handler)
	if err != nil {
		return nil, err
	}
	var args []string
	executable := handlerSplit[0]
	if len(handlerSplit) > 1 {
		args = handlerSplit[1:]
	}
	path, err := exec.LookPath(executable)
	if err == nil {
		executable = path
	}
	execPath, err := filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("unable to locate handler: %s", executable)
	}
	return exec.Command(execPath, append(args, appendArgs...)...), nil
}

func HandleAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if *AuthHook == "" {
		// allow all
		return &ssh.Permissions{
			Extensions: map[string]string{
				"user": conn.User(),
			},
		}, nil
	}

	pubKey := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
	cmd, err := handlerCmd(*AuthHook, conn.User(), pubKey)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	status, err := exitStatus(cmd.Run())
	if err != nil {
		return nil, err
	}
	if status.Status == 0 {
		return &ssh.Permissions{
			Extensions: map[string]string{
				"environ": strings.Trim(output.String(), "\n"),
				"user":    conn.User(),
			},
		}, nil
	}
	Debug("authentication hook status:", status.Status)
	return nil, fmt.Errorf("authentication failed")
}

func HandleChannel(conn *ssh.ServerConn, newChan ssh.NewChannel) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Println("newChan.Accept failed:", err)
		return
	}

	// Setup stdout/stderr
	var stdout, stderr io.Writer
	if *DebugMode {
		stdout = io.MultiWriter(ch, os.Stdout)
		stderr = io.MultiWriter(ch.Stderr(), os.Stdout)
	} else {
		stdout = ch
		stderr = ch.Stderr()
	}

	handler := sshHandler{
		channel: ch,
		stdout:  stdout,
		stderr:  stderr,
	}

	// Load default environment
	if *UseEnv {
		handler.Env = os.Environ()
	}
	if conn.Permissions != nil {
		// Using Permissions.Extensions as a way to get state from PublicKeyCallback
		if conn.Permissions.Extensions["environ"] != "" {
			handler.Env = append(handler.Env, strings.Split(conn.Permissions.Extensions["environ"], "\n")...)
		}
		handler.Env = append(handler.Env, "USER="+conn.Permissions.Extensions["user"])
	}

	for req := range reqs {
		go handler.Request(req)
	}
}

// sshHandler is a stateful handler for requests within an SSH channel
type sshHandler struct {
	sync.Mutex
	Env      []string
	channel  ssh.Channel
	stdout   io.Writer
	stderr   io.Writer
	ptyShell *os.File
}

func (h *sshHandler) assert(at string, err error) bool {
	if err != nil {
		log.Printf("%s failed: %s", at, err)
		h.stderr.Write([]byte("Internal error.\n"))
		return true
	}
	return false
}

// Exit sends an exit-status request to the channel based on the err.
func (h *sshHandler) Exit(err error) error {
	defer h.channel.Close()

	status, err := exitStatus(err)
	if !h.assert("exit", err) {
		_, err := h.channel.SendRequest("exit-status", false, ssh.Marshal(&status))
		h.assert("status", err)
		return err
	}
	return err
}

type EnvVar struct {
	Name, Value string
}

func (h *sshHandler) Request(req *ssh.Request) {
	switch req.Type {
	case "exec":
		h.handleExec(req)
	case "env":
		h.handleEnv(req)
	case "pty-req":
		h.handlePty(req)
	case "window-change":
		h.handleWinch(req)
	default:
		if req.WantReply {
			req.Reply(true, nil)
		}
	}
}

func (h *sshHandler) handleEnv(req *ssh.Request) {
	var pair EnvVar
	ssh.Unmarshal(req.Payload, &pair)
	envvar := fmt.Sprintf("%s=%s", pair.Name, pair.Value)
	h.Env = append(h.Env, envvar)
	req.Reply(true, nil)
}

// handleExec when executed a command e.g.
// ssh user@localhost -p 2222 'echo Hello'
func (h *sshHandler) handleExec(req *ssh.Request) {
	h.Lock()
	defer h.Unlock()

	var payload = struct{ Value string }{}
	ssh.Unmarshal(req.Payload, &payload)
	cmdargs, err := shlex.Split(payload.Value)
	if err != nil {
		Debug("failed exec split:", err)
		h.channel.Close()
		return
	}

	cmd, err := handlerCmd(flag.Arg(0), cmdargs...)
	if err != nil {
		Debug("failed handler init:", err)
		h.channel.Close()
		return
	}
	cmd.Env = append(h.Env, "SSH_ORIGINAL_COMMAND="+strings.Join(cmdargs, " "))
	cmd.Stdout = h.stdout
	cmd.Stderr = h.stderr

	// cmd.Wait closes the stdin when it's done, so we need to proxy it through a pipe
	stdinPipe, err := cmd.StdinPipe()
	if h.assert("exec cmd.StdinPipe", err) {
		h.channel.Close()
		return
	}
	go func() {
		defer stdinPipe.Close()
		io.Copy(stdinPipe, h.channel)
	}()

	if req.WantReply {
		req.Reply(true, nil)
	}

	// We run inline to prevent concurrent exec requests for the channel as the lock is held.
	h.Exit(cmd.Run())
}

// handlePty when executed a command e.g.
// ssh user@localhost -p 2222
func (h *sshHandler) handlePty(req *ssh.Request) {
	h.Lock()
	defer h.Unlock()

	if h.ptyShell != nil {
		// Only allow one pty per channel
		req.Reply(false, nil)
		return
	}

	width, height, okSize := parsePtyRequest(req.Payload)

	scriptName := flag.Arg(0)
	cmd, err := handlerCmd(scriptName)
	if err != nil {
		Debug("failed handler init:", err)
		h.channel.Close()
		return
	}
	cmd.Env = h.Env

	// attachShell does cmd.Start() so we need to do cmd.Wait() later
	ptyShell, _, err := attachShell(cmd, h.stdout, h.channel)
	if h.assert("pty attachShell", err) {
		h.channel.Close()
		return
	}
	h.ptyShell = ptyShell

	if okSize {
		setWinsize(ptyShell.Fd(), width, height)
	}

	// Ready to receive input
	req.Reply(true, nil)

	// We run this concurrently so that the lock is released for window-change events.
	go h.Exit(cmd.Wait())
}

func (h *sshHandler) handleWinch(req *ssh.Request) {
	h.Lock()
	defer h.Unlock()

	width, height, okSize := parsePtyRequest(req.Payload)
	if okSize && h.ptyShell != nil {
		setWinsize(h.ptyShell.Fd(), width, height)
	}
}

func attachShell(cmd *exec.Cmd, stdout io.Writer, stdin io.Reader) (*os.File, *sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Note that pty merges stdout and stderr.
	cmdPty, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, err
	}
	go func() {
		io.Copy(stdout, cmdPty)
		wg.Done()
	}()
	go func() {
		io.Copy(cmdPty, stdin)
		wg.Done()
	}()

	return cmdPty, &wg, nil
}
