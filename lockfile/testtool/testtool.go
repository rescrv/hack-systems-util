package main

import (
	"time"

	"hack.systems/util/lockfile"
)

func main() {
	L, err := lockfile.Lock("foo")
	if err != nil {
		panic(err)
	}
	for {
		time.Sleep(time.Second)
	}
	L.Unlock()
}
