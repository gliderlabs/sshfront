package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

var Version string

var (
	listenHost = flag.String("h", "0.0.0.0", "ip to listen on")
	listenPort = flag.String("p", "22", "port to listen on")
	hostKey    = flag.String("k", "~/.ssh/id_rsa", "private host key path")
	authHook   = flag.String("a", "", "authentication hook. empty=allow all")
	debugMode  = flag.Bool("d", false, "debug mode")
	useEnv     = flag.Bool("e", false, "pass environment to handler")
)

func debug(v ...interface{}) {
	if *debugMode {
		log.Println(v...)
	}
}

func handleConn(conn net.Conn, conf *ssh.ServerConfig) {
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, conf)
	if err != nil {
		debug("handshake failed:", err)
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unsupported channel type")
			debug("channel rejected, unsupported type:", ch.ChannelType())
			continue
		}
		go handleChannel(sshConn, ch)
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
			return handleAuth(conn, key)
		},
	}
	setupHostKey(config)

	var listenAddr string
	if listenAddr = os.Getenv("SSHFRONT_LISTEN"); listenAddr == "" {
		listenAddr = fmt.Sprintf("%s:%s", *listenHost, *listenPort)
	}

	log.Printf("sshfront v%s listening on %s ...\n", Version, listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalln("listen failed:", err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			debug("accept failed:", err)
			continue
		}
		go handleConn(conn, config)
	}
}
