package main

import (
	"fmt"
	"os"
	"qa/cmd/flamegraph"
	"qa/cmd/run"
	"qa/cmd/stackcollapse"
)

func main() {
	var status int
	command := os.Args[1]
	switch command {
	case "run":
		status = run.Main(os.Args[2:])
	case "flamegraph":
		err := flamegraph.Main(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		} else {
			status = 0
		}
	case "stackcollapse":
		err := stackcollapse.Main(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		} else {
			status = 0
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		status = 1
	}

	os.Exit(status)
}
