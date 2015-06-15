package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flynn/go-shlex"
	"golang.org/x/crypto/ssh"
)

var host = flag.String("h", "", "host ip to listen on")
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
	var output bytes.Buffer

	keydata := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
	cmd := exec.Command(handler[0], append(handler[1:], conn.User(), keydata)...)
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
	listener, err := net.Listen("tcp", *host+":"+*port)
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
