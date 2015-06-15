package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/flynn/go-shlex"
	"github.com/kr/pty"
	"golang.org/x/crypto/ssh"
)

func handleChannel(conn *ssh.ServerConn, newChan ssh.NewChannel, execHandler []string) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Println("newChan.Accept failed:", err)
		return
	}

	// Setup stdout/stderr
	var stdout, stderr io.Writer
	if *debug {
		stdout = io.MultiWriter(ch, os.Stdout)
		stderr = io.MultiWriter(ch.Stderr(), os.Stdout)
	} else {
		stdout = ch
		stderr = ch.Stderr()
	}

	handler := sshHandler{
		ExecHandler: execHandler,
		channel:     ch,
		stdout:      stdout,
		stderr:      stderr,
	}

	// Load default environment
	if *env {
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
	Env         []string
	ExecHandler []string
	channel     ssh.Channel
	stdout      io.Writer
	stderr      io.Writer
	ptyShell    *os.File
}

func (h sshHandler) assert(at string, err error) bool {
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

func (h *sshHandler) Request(req *ssh.Request) {
	switch req.Type {
	case "exec":
		h.handleExec(req)
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

func (h *sshHandler) handleExec(req *ssh.Request) {
	h.Lock()
	defer h.Unlock()

	cmdline := string(req.Payload[4:])

	// Initialize Cmd
	var cmd *exec.Cmd
	if *shell {
		shellcmd := flag.Arg(1) + " " + cmdline
		cmd = exec.Command(os.Getenv("SHELL"), "-c", shellcmd)
	} else {
		cmdargs, err := shlex.Split(cmdline)
		if h.assert("exec shlex.Split", err) {
			h.channel.Close()
			return
		}
		cmd = exec.Command(h.ExecHandler[0], append(h.ExecHandler[1:], cmdargs...)...)
	}

	cmd.Env = append(h.Env, "SSH_ORIGINAL_COMMAND="+cmdline)
	cmd.Stdout = h.stdout
	cmd.Stderr = h.stderr

	// cmd.Wait closes the stdin when it's done, so we need to proxy it through a pipe
	stdinPipe, err := cmd.StdinPipe()
	if h.assert("exec cmd.StdinPipe", err) {
		h.channel.Close()
		return
	}
	go io.Copy(stdinPipe, h.channel)

	if req.WantReply {
		req.Reply(true, nil)
	}

	// We run inline to prevent concurrent exec requests for the channel as the lock is held.
	h.Exit(cmd.Run())
}

func (h *sshHandler) handlePty(req *ssh.Request) {
	h.Lock()
	defer h.Unlock()

	if h.ptyShell != nil {
		// Only allow one pty per channel
		req.Reply(false, nil)
		return
	}

	width, height, okSize := parsePtyRequest(req.Payload)

	// Initialize Cmd
	var cmd *exec.Cmd
	if *shell {
		cmd = exec.Command(os.Getenv("SHELL"))
	} else {
		cmd = exec.Command(h.ExecHandler[0], h.ExecHandler[1:]...)
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

func (h sshHandler) handleWinch(req *ssh.Request) {
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
