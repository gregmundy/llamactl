package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fmt.Println("loaded model")
	// Honor --version probe.
	for _, a := range os.Args[1:] {
		if a == "--version" {
			fmt.Println("version: test")
			fmt.Println("build: 99999")
			return
		}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
		fmt.Println("shutting down")
	case <-time.After(30 * time.Second):
		fmt.Println("timeout")
	}
}
