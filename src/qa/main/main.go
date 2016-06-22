package main

import (
	"errors"
	"fmt"
	"os"
	"qa/cmd/flamegraph"
	"qa/cmd/run"
	"qa/cmd/stackcollapse"
)

func main() {
	var status int
	command := os.Args[1]
	var err error
	switch command {
	case "run":
		dir, err := os.Getwd()
		if err == nil {
			err = run.Main(os.Stdout, os.Stderr, dir, os.Args[2:])
		}
	case "flamegraph":
		err = flamegraph.Main(os.Args[2:])
	case "stackcollapse":
		err = stackcollapse.Main(os.Args[2:])
	default:
		err = errors.New("Unknown command: " + command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	} else {
		status = 0
	}

	os.Exit(status)
}
