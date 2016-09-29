package flaky

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os/user"
	"path"
	"qa/cmd"
	"qa/cmd/flaky/repro"
	"qa/cmd/flaky/top"
	"qa/flaky"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/mattn/go-shellwords"
)

func runSubcommand(session *flaky.Session, env *cmd.Env, argv []string) error {
	command := argv[0]
	argv[0] = session.ProgramName + " " + command

	switch command {
	case "top":
		stdinBuf := &bytes.Buffer{}
		encoder := json.NewEncoder(stdinBuf)
		summaries, err := session.Summaries()
		if err != nil {
			return fmt.Errorf("%s: %s", argv[0], err)
		}

		for _, summary := range summaries {
			encoder.Encode(summary)
		}

		return top.Main(
			&cmd.Env{Stdin: bytes.NewBuffer(stdinBuf.Bytes()), Stdout: env.Stdout, Stderr: env.Stderr},
			argv,
		)
	case "repro":
		return repro.Main(session, env, argv)
	default:
		return fmt.Errorf("%s: command not found: %s", session.ProgramName, command)
	}
}

func runRepl(session *flaky.Session, env *cmd.Env) error {
	var completer = readline.NewPrefixCompleter(
		readline.PcItem("top"),
		readline.PcItem("repro"),
		readline.PcItem("list"),
	)

	l, err := readline.NewEx(&readline.Config{
		Stdin:           env.Stdin,
		Stdout:          env.Stdout,
		Stderr:          env.Stderr,
		Prompt:          "\033[31m»\033[0m ",
		HistoryFile:     "/tmp/readline.tmp",
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		panic(err)
	}

	defer l.Close()

	log.SetOutput(l.Stderr())
	previousLine := ""
	for {
		line, err := l.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		}

		line = strings.TrimSpace(line)
		if line == "!!" {
			line = previousLine
			fmt.Fprintf(env.Stderr, "%s\n", line)
		}

		if line == "" {
			continue
		}

		previousLine = line

		argv, err := shellwords.Parse(line)
		if err != nil {
			fmt.Fprintf(env.Stderr, "bad syntax: %s\n", err)
			continue
		}

		err = runSubcommand(session, env, argv)
		if err == nil {
			continue
		}

		if _, ok := err.(*cmd.QuietError); ok {
			continue
		}

		fmt.Fprintf(env.Stderr, "%s\n", err)
	}

	return nil
}

func Main(env *cmd.Env, argv []string) error {
	flags := flag.NewFlagSet(argv[0], flag.ContinueOnError)

	usr, err := user.Current()
	if err != nil {
		return err
	}

	archiveBaseDirDefault := env.Vars["QA_ARCHIVE"]
	if archiveBaseDirDefault == "" {
		archiveBaseDirDefault = path.Join(usr.HomeDir, ".qa", "archive")
	}

	archiveBaseDir := flags.String("archive", archiveBaseDirDefault, "Base directory to store data for later analysis")

	numDays := flags.Int("days-back", 7, "Number of days to search backwards from -until-date")

	now := time.Now()
	untilDate := flags.String("until-date", now.Format("2006-01-02"), "Date (YYYY-MM-DD) to search -archive backwards from")

	err = flags.Parse(argv[1:])
	if err != nil {
		return err
	}

	session := &flaky.Session{
		ArchiveBaseDir: *archiveBaseDir,
		NumDays:        *numDays,
		UntilDate:      *untilDate,
		Stderr:         env.Stderr,
		ProgramName:    argv[0],
	}

	if err != nil {
		return err
	}

	args := flags.Args()
	if len(args) > 0 {
		return runSubcommand(session, env, args)
	}

	return runRepl(session, env)
}
