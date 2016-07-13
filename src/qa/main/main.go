package main

import (
	"errors"
	"fmt"
	"os"
	"qa/cmd"
	"qa/cmd/discover"
	"qa/cmd/flaky"
	"qa/cmd/grouping"
	"qa/cmd/summary"
	"qa/cmd/flamegraph"
	"qa/cmd/run"
	"qa/cmd/stackcollapse"
)

func main() {
	var status int
	command := os.Args[1]
	var err error
	switch command {
	case "flaky":
		env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		err = flaky.Main(env, os.Args[2:])
	case "discover":
		env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		err = discover.Main(env, os.Args[2:])
	case "grouping":
		env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		err = grouping.Main(env, os.Args[2:])
	case "summary":
		env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		err = summary.Main(env, os.Args[2:])
	case "run":
		env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		err = run.Main(env, os.Args[2:])
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
