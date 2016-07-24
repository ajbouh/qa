package flaky

import (
	"flag"
	"fmt"
	"io"
	"os/user"
	"path"
	"qa/cmd"
	"qa/cmd/discover"
	"qa/cmd/grouping"
	"qa/cmd/summary"
	"strconv"
	"sync"
	"time"
)

type pipelineOp struct {
	main func(*cmd.Env, []string) error
	args []string
}

func runPipeline(env *cmd.Env, ops []pipelineOp) error {
	errChan := make(chan error, len(ops))

	stderrRd, stderrWr := io.Pipe()
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	defer stderrRd.Close()
	defer stderrWr.Close()
	go func() {
		defer stderrWg.Done()
		io.Copy(env.Stderr, stderrRd)
	}()

	stdin := env.Stdin
	for ix, op := range ops {

		var opEnv *cmd.Env
		var closer io.Closer
		if ix == len(ops)-1 {
			opEnv = &cmd.Env{Stdin: stdin, Stdout: env.Stdout, Stderr: stderrWr}
		} else {
			rd, wr := io.Pipe()
			defer rd.Close()
			defer wr.Close()
			opEnv = &cmd.Env{Stdin: stdin, Stdout: wr, Stderr: stderrWr}
			stdin = rd
			closer = wr
		}

		go func(opEnv *cmd.Env, op pipelineOp, closer io.Closer) {
			if closer != nil {
				defer closer.Close()
			}
			errChan <- op.main(opEnv, op.args)
		}(opEnv, op, closer)
	}

	errs := []error{}
	receiveCount := 0
	for err := range errChan {
		receiveCount++

		if err != nil {
			errs = append(errs, err)
		}

		if receiveCount == len(ops) {
			close(errChan)
		}
	}

	stderrWr.Close()

	stderrWg.Wait()

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf("Pipeline failed: %v", errs)
}

func Main(env *cmd.Env, args []string) error {
	flags := flag.NewFlagSet("flaky", flag.ContinueOnError)

	usr, err := user.Current()
	if err != nil {
		return err
	}

	archiveBaseDirDefault := path.Join(usr.HomeDir, ".qa", "archive")
	archiveBaseDir := flags.String("archive-base-dir", archiveBaseDirDefault, "Base directory to store data for later analysis")
	filterCollapseId := flags.String("filter-collapse-id", "suite.coderef,suite.label,case-labels,label", "Collapse id to use to consolidate tests")
	summaryCollapseId := flags.String("summary-collapse-id", "suite.label,case-labels,label", "Collapse id to use to consolidate tests")
	numDays := flags.Int("days-back", 7, "Number of days to search backwards from -until-date")
	format := flags.String("format", "pretty", "Format to display summary in, options are: pretty, json")
	showAces := flags.Bool("show-aces", false, "Whether or not to show tests that always pass")

	now := time.Now()
	untilDate := flags.String("until-date", now.Format("2006-01-02"), "Date (YYYY-MM-DD) to search -archive-base-dir backwards from")

	err = flags.Parse(args)
	if err != nil {
		return err
	}

	var acesArg string
	if *showAces {
		acesArg = "--show-aces"
	} else {
		acesArg = "--no-show-aces"
	}

	return runPipeline(
		env,
		[]pipelineOp{
			pipelineOp{
				main: discover.Main,
				args: []string{
					"--dir", *archiveBaseDir,
					"--number-days", strconv.Itoa(*numDays),
					"--until-date", *untilDate,
				},
			},
			// pipelineOp{
			// 	main: grouping.Main,
			// 	args: []string{
			// 		"--collapse-id", *collapseId,
			// 		"--keep-if-any", "status==\"fail\"",
			// 		"--keep-if-any", "status==\"error\"",
			// 	},
			// },
			pipelineOp{
				main: grouping.Main,
				args: []string{
					"--collapse-id", *filterCollapseId,
					"--keep-if-any", "status==\"pass\"",
					"--keep-residual-records-matching-kept", "outcome-digest",
				},
			},
			pipelineOp{
				main: summary.Main,
				args: []string{
					"--format", *format,
					acesArg,
					"--duration", "time",
					"--sort-by", "suite.start",
					"--group-by", *summaryCollapseId,
					"--subgroup-by", "outcome-digest",
					"--ignore-if", "status==\"todo\"",
					"--ignore-if", "status==\"omit\"",
					"--success-if", "status==\"pass\"",
				},
			},
		},
	)
}
