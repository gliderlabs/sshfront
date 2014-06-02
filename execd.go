package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"code.google.com/p/go.crypto/ssh"
	"github.com/flynn/go-shlex"
)

var port = flag.String("p", "22", "port to listen on")
var debug = flag.Bool("d", false, "debug mode displays handler output")
var env = flag.Bool("e", false, "pass environment to handlers")
var shell = flag.Bool("s", false, "run exec handler via SHELL")
var keys = flag.String("k", "", "pem file of private keys (read from SSH_PRIVATE_KEYS by default)")

var ErrUnauthorized = errors.New("execd: user is unauthorized")

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

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(2)

	if stdin != nil {
		stdinIn, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		go func() {
			io.Copy(stdinIn, stdin)
			stdinIn.Close()
		}()
	}

	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	go func() {
		io.Copy(stdout, stdoutOut)
		wg.Done()
	}()

	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go func() {
		io.Copy(stderr, stderrOut)
		wg.Done()
	}()

	return &wg, nil
}

func addKey(conf *ssh.ServerConfig, block *pem.Block) (err error) {
	var key interface{}
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "DSA PRIVATE KEY":
		key, err = ssh.ParseDSAPrivateKey(block.Bytes)
	default:
		return fmt.Errorf("unsupported key type %q", block.Type)
	}
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return err
	}
	conf.AddHostKey(signer)
	return nil
}

func parseKeys(conf *ssh.ServerConfig, pemData []byte) error {
	var found bool
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			if !found {
				return errors.New("no private keys found")
			}
			return nil
		}
		if err := addKey(conf, block); err != nil {
			return err
		}
		found = true
	}
}

func handleAuth(handler []string, conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	keydata := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
	cmd := exec.Command(handler[0], append(handler[1:], conn.User(), keydata)...)
	var output bytes.Buffer
	done, err := attachCmd(cmd, &output, &output, nil)
	if err != nil {
		return nil, err
	}
	err = cmd.Run()
	done.Wait()
	status, err := exitStatus(err)
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
	} else {
		log.Println("auth-handler status:", status.Status)
	}
	return nil, ErrUnauthorized
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <auth-handler> <exec-handler>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(64)
	}

	execHandler, err := shlex.Split(flag.Arg(1))
	if err != nil {
		log.Fatalln("Unable to parse receiver command:", err)
	}
	execHandler[0], err = filepath.Abs(execHandler[0])
	if err != nil {
		log.Fatalln("Invalid receiver path:", err)
	}

	authHandler, err := shlex.Split(flag.Arg(0))
	if err != nil {
		log.Fatalln("Unable to parse authchecker command:", err)
	}
	authHandler[0], err = filepath.Abs(authHandler[0])
	if err != nil {
		log.Fatalln("Invalid authchecker path:", err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return handleAuth(authHandler, conn, key)
		},
	}

	if keyEnv := os.Getenv("SSH_PRIVATE_KEYS"); keyEnv != "" {
		if err := parseKeys(config, []byte(keyEnv)); err != nil {
			log.Fatalln("Failed to parse private keys:", err)
		}
	} else {
		pemBytes, err := ioutil.ReadFile(*keys)
		if err != nil {
			log.Fatalln("Failed to load private keys:", err)
		}
		if err := parseKeys(config, pemBytes); err != nil {
			log.Fatalln("Failed to parse private keys:", err)
		}
	}

	if p := os.Getenv("PORT"); p != "" && *port == "22" {
		*port = p
	}
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalln("Failed to listen for connections:", err)
	}
	for {
		// SSH connections just house multiplexed connections
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept incoming connection:", err)
			continue
		}
		go handleConn(conn, config, execHandler)
	}
}

func handleConn(conn net.Conn, conf *ssh.ServerConfig, execHandler []string) {
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, conf)
	if err != nil {
		log.Println("Failed to handshake:", err)
		return
	}

	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		go handleChannel(sshConn, ch, execHandler)
	}
}

func handleChannel(conn *ssh.ServerConn, newChan ssh.NewChannel, execHandler []string) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Println("newChan.Accept failed:", err)
		return
	}
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			assert := func(at string, err error) bool {
				if err != nil {
					log.Printf("%s failed: %s", at, err)
					ch.Stderr().Write([]byte("Internal error.\n"))
					return true
				}
				return false
			}

			if req.WantReply {
				req.Reply(true, nil)
			}

			cmdline := string(req.Payload[4:])
			var cmd *exec.Cmd
			if *shell {
				shellcmd := flag.Arg(1) + " " + cmdline
				cmd = exec.Command(os.Getenv("SHELL"), "-c", shellcmd)
			} else {
				cmdargs, err := shlex.Split(cmdline)
				if assert("shlex.Split", err) {
					return
				}
				cmd = exec.Command(execHandler[0], append(execHandler[1:], cmdargs...)...)
			}
			if !*env {
				cmd.Env = []string{}
			}
			if conn.Permissions != nil {
				// Using Permissions.Extensions as a way to get state from PublicKeyCallback
				if conn.Permissions.Extensions["environ"] != "" {
					cmd.Env = append(cmd.Env, strings.Split(conn.Permissions.Extensions["environ"], "\n")...)
				}
				cmd.Env = append(cmd.Env, "USER="+conn.Permissions.Extensions["user"])
			}
			cmd.Env = append(cmd.Env, "SSH_ORIGINAL_COMMAND="+cmdline)
			var stdout, stderr io.Writer
			if *debug {
				stdout = io.MultiWriter(ch, os.Stdout)
				stderr = io.MultiWriter(ch.Stderr(), os.Stdout)
			} else {
				stdout = ch
				stderr = ch.Stderr()
			}
			done, err := attachCmd(cmd, stdout, stderr, ch)
			if assert("attachCmd", err) {
				return
			}
			if assert("cmd.Start", cmd.Start()) {
				return
			}
			done.Wait()
			status, err := exitStatus(cmd.Wait())
			if assert("exitStatus", err) {
				return
			}
			_, err = ch.SendRequest("exit-status", false, ssh.Marshal(&status))
			assert("sendExit", err)
			return
		case "env":
			if req.WantReply {
				req.Reply(true, nil)
			}
		default:
			return
		}
	}
}
