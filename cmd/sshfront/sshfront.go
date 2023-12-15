package main

import (
	"flag"
	"fmt"
	. "github.com/gliderlabs/sshfront/internal"
	"log"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

var Version string

func handleConn(conn net.Conn, conf *ssh.ServerConfig) {
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, conf)
	if err != nil {
		Debug("handshake failed:", err)
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unsupported channel type")
			Debug("channel rejected, unsupported type:", ch.ChannelType())
			continue
		}
		go HandleChannel(sshConn, ch)
	}
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <handler>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(64)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return HandleAuth(conn, key)
		},
	}
	SetupHostKey(config)

	listenAddr := os.Getenv("SSHFRONT_LISTEN")
	if listenAddr == "" {
		listenAddr = net.JoinHostPort(*ListenHost, *ListenPort)
	}

	log.Printf("sshfront v%s listening on %s ...\n", Version, listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalln("listen failed:", err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			Debug("accept failed:", err)
			continue
		}
		go handleConn(conn, config)
	}
}
