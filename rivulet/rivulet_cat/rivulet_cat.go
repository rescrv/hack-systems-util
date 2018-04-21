package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"hack.systems/util/rivulet"
)

func usage() {
	log.Printf("Usage: rivulet_cat <remote address>")
}

func main() {
	log.SetFlags(0)
	flag.CommandLine.Usage = usage
	flag.Parse()
	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	c := rivulet.Connect("tcp", flag.Arg(0))
	done := uint32(0)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if atomic.LoadUint32(&done) != 0 {
				return
			}
			s, err := c.Recv()
			if err == rivulet.HUP {
				return
			} else if err != nil {
				log.Fatalf("recv error: %s", err)
			} else {
				fmt.Printf("%s", s)
			}
		}
	}()
	stdin := bufio.NewReader(os.Stdin)
	for {
		line, err := stdin.ReadString('\n')
		if err == io.EOF {
			atomic.StoreUint32(&done, 1)
			err = c.HangUp()
			if err != nil {
				log.Fatalf("hangup error: %s", err)
			}
			break
		} else if err != nil {
			log.Fatalf("stdin error: %s", err)
		}
		err = c.Send(line)
		if err != nil {
			log.Fatalf("send error: %s", err)
		}
	}
	wg.Wait()
	err := c.Reset()
	if err != nil {
		log.Fatalf("close error: %s")
	}
}
