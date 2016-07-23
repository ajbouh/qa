package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"qa/cmd"
	"qa/cmd/auto"
	"qa/cmd/discover"
	"qa/cmd/flaky"
	"qa/cmd/flamegraph"
	"qa/cmd/grouping"
	"qa/cmd/run"
	"qa/cmd/stackcollapse"
	"qa/cmd/summary"
)

func main() {
	var status int
	command := os.Args[1]
	env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}

	var err error
	switch command {
	case "flaky":
		err = flaky.Main(env, os.Args[2:])
	case "discover":
		err = discover.Main(env, os.Args[2:])
	case "grouping":
		err = grouping.Main(env, os.Args[2:])
	case "summary":
		err = summary.Main(env, os.Args[2:])
	case "auto":
		err = auto.Main(env, os.Args[2:])
	case "run":
		err = run.Main(env, os.Args[2:])
	case "flamegraph":
		// TODO(adamb) Switch flamegraph to use env arg
		err = flamegraph.Main(os.Args[2:])
	case "stackcollapse":
		// TODO(adamb) Switch stackcollapse to use env arg
		err = stackcollapse.Main(os.Args[2:])
	default:
		err = errors.New("Unknown command: " + command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1

		if exitError, ok := err.(*exec.ExitError); ok {
			if len(exitError.Stderr) > 0 {
				fmt.Fprintln(env.Stderr, string(exitError.Stderr))
			}
		}
	} else {
		status = 0
	}

	os.Exit(status)
}
