package main

import (
	"flag"
	"log"
	"os"

	"hack.systems/util/rivulet"
)

func usage() {
	log.Printf("Usage: rivulet_echo <local address>")
}

func handle(conn *rivulet.Connection) {
	defer conn.Close()
	for {
		s, err := conn.Recv()
		if err == rivulet.HUP {
			conn.HangUp()
			return
		} else if err != nil {
			log.Printf("error: %s", err)
			return
		}
		err = conn.Send(s)
		if err != nil {
			log.Printf("error: %s", err)
			return
		}
	}
}

func main() {
	log.SetFlags(0)
	flag.CommandLine.Usage = usage
	flag.Parse()
	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Llongfile | log.LUTC)
	s, err := rivulet.NewServer(flag.Arg(0))
	if err != nil {
		log.Fatalf("bootstrap error: %s", err)
	}
	for {
		conn, err := s.Accept()
		if err != nil {
			log.Printf("accept error: %s", err)
			continue
		}
		go handle(conn)
	}
}
