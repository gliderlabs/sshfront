package internal

import (
	"flag"
	"log"
)

var (
	ListenHost = flag.String("h", "0.0.0.0", "ip to listen on")
	ListenPort = flag.String("p", "22", "port to listen on")
	HostKey    = flag.String("k", "~/.ssh/id_rsa", "private host key path")
	AuthHook   = flag.String("a", "", "authentication hook. empty=allow all")
	DebugMode  = flag.Bool("d", false, "debug mode")
	UseEnv     = flag.Bool("e", false, "pass environment to handler")
)

func Debug(v ...interface{}) {
	if *DebugMode {
		log.Println(v...)
	}
}
