package main

import (
	"os"
)

func main() {
	if os.Getenv(envDaemon) == "1" {
		os.Exit(runDaemon())
	}
	os.Exit(runClient(os.Args[1:]))
}
