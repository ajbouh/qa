package main

import (
	"fmt"
	"os"
	"os/exec"
	"qa/cmd"
	"qa/cmd/discover"
	"qa/cmd/flaky"
	"qa/cmd/flamegraph"
	"qa/cmd/grouping"
	"qa/cmd/run"
	"qa/cmd/stackcollapse"
	"qa/cmd/summary"
	"sort"
)

type subcommand struct {
	main        func(*cmd.Env, []string) error
	documented  bool
	description string
}

const overview = `qa is a lightweight tool for running your tests fast.`

var subcommands = map[string]subcommand {
	"flaky": subcommand{
		documented: true,
		main: flaky.Main,
		description: "Start REPL for finding and fixing flaky tests",
	},
	"discover": subcommand{
		main: discover.Main,
		description: "Emit a stream of outcome-digest and case-labels augmented test events to stdout",
	},
	"grouping": subcommand{
		main: grouping.Main,
		description: "Filter stream of test events, emitting subset to stdout",
	},
	"summary": subcommand{
		main: summary.Main,
		description: "Summarize outcomes of stream of test events",
	},
	"run": subcommand{
		main: run.Main,
		description: "Run one or more test runners",
	},
	"rspec": subcommand{
		documented: true,
		main: func(env *cmd.Env, argv []string) error {
				return run.Framework("rspec", env, argv)
			},
			description: "Run RSpec specs",
		},
	"minitest": subcommand{
		documented: true,
		main: func(env *cmd.Env, argv []string) error {
				return run.Framework("minitest", env, argv)
			},
			description: "Run Minitest tests",
		},
	"test-unit": subcommand{
		documented: true,
		main: func(env *cmd.Env, argv []string) error {
				return run.Framework("test-unit", env, argv)
			},
			description: "Run Test::Unit tests",
		},
	"flamegraph": subcommand{
		main: flamegraph.Main,
		description: "Generate a flamegraph from an enriched TAP-J stream",
	},
	"stackcollapse": subcommand{
		main: stackcollapse.Main,
		description: "Generate a stackcollapse from an enriched TAP-J stream",
	},
	"help": subcommand{
		documented: true,
		description: "This usage text",
	},
}

func usage(env *cmd.Env, argv []string) error {
	subcommandNames := make([]string, 0, len(subcommands))
	for subcommandName, subcommand := range subcommands {
		if !subcommand.documented {
			continue
		}
		subcommandNames = append(subcommandNames, subcommandName)
	}

	sort.Strings(subcommandNames)

	fmt.Fprintf(env.Stdout, "usage: %s <command> [<args>]\n\n", os.Args[0])
	fmt.Fprintf(env.Stdout, "%s\n\n", overview)
	fmt.Fprintf(env.Stdout, "These are common qa commands used in various situations:\n")
	for _, subcommandName := range subcommandNames {
		subcommand := subcommands[subcommandName]
		fmt.Fprintf(env.Stdout, "  %-11s %s\n", subcommandName, subcommand.description)
	}

	return nil
}

func Main(env *cmd.Env, argv []string) error {
	command := argv[1]

	if command == "-h" || command == "-help" || command == "--help" || command == "help" {
		return usage(env, argv)
	}

	if subcommand, ok := subcommands[command]; ok {
		return subcommand.main(env, append([]string{argv[0] + " " + argv[1]}, argv[2:]...))
	} else {
		return fmt.Errorf("Unknown command: %s. Try: qa help", command)
	}
}

func main() {
	var status int

	env := &cmd.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	err := Main(env, os.Args)

	if err != nil {
		if quietError, ok := err.(*cmd.QuietError); ok {
			status = quietError.Status
		} else {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}

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
