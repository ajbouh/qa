package main

// cd <basedir> && qa

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"qa/runner"
	"qa/tapj"
	"qa/tapj/reporters"
)

type testResult struct {
	testCases []tapj.CaseEvent
	testEvent tapj.TestEvent
	testError error
}

func parallelTestRun(
	runDecodingCallbacks *tapj.DecodingCallbacks,
	seed int,
	testRunners []runner.TestRunner,
	maxJobs int) (final tapj.FinalEvent, err error) {
	startTime := time.Now().UTC()

	suite := tapj.SuiteEvent{
		Type:  "suite", // TODO(adamb) Figure out how to make Type implied.
		Start: startTime.Format("2006-01-02 15:04:05"),
		Count: len(testRunners),
		Seed:  seed,
		Rev:   4,
	}

	final = tapj.FinalEvent{
		Type: "final", // TODO(adamb) Figure out how to make Type implied.
	}

	err = runDecodingCallbacks.OnSuite(suite)
	if err != nil {
		return
	}

	defer func() {
		final.Time = time.Now().UTC().Sub(startTime).Seconds()

		finalErr := runDecodingCallbacks.OnFinal(suite, final)
		if err == nil {
			err = finalErr
		}
	}()

	var testRunnerChan = make(chan runner.TestRunner, maxJobs)
	var sem = make(chan int, maxJobs)

	go func() {
		for _, testRunner := range testRunners {
			sem <- 1
			testRunnerChan <- testRunner
		}
	}()

	var testResultChan = make(chan testResult, maxJobs)

	var abort = false
	for final.Counts.Total < len(testRunners) {
		select {
		case testRunner := <-testRunnerChan:
			if abort {
				testResultChan <- testResult{testError: errors.New("already aborted")}
			} else {
				go func() {
					cases, test, err := testRunner.Run()
					testResultChan <- testResult{cases, test, err}
				}()
			}
		case testResult := <-testResultChan:
			<-sem
			err := testResult.testError
			if err == nil {
				err = runDecodingCallbacks.OnTest(testResult.testCases, testResult.testEvent)
			}

			final.Counts.Total += 1

			if err != nil {
				abort = true
				final.Counts.Error += 1
				fmt.Fprintln(os.Stderr, "Error:", err)
				break
			}

			test := testResult.testEvent
			switch test.Status {
			case tapj.Pass:
				final.Counts.Pass += 1
			case tapj.Fail:
				final.Counts.Fail += 1
			case tapj.Error:
				final.Counts.Error += 1
			case tapj.Omit:
				final.Counts.Omit += 1
			case tapj.Todo:
				final.Counts.Todo += 1
			}
		}
	}

	return
}

func main() {
	format := flag.String("format", "pretty", "Set output format")
	jobs := flag.Int("jobs", runtime.NumCPU(), "Set number of jobs")
	flag.Parse()

	var runDecodingCallbacks *tapj.DecodingCallbacks

	switch *format {
	case "tapj":
		currentCases := []tapj.CaseEvent{}
		runDecodingCallbacks = &tapj.DecodingCallbacks{
			OnSuite: func(suite tapj.SuiteEvent) (err error) {
				var s []byte
				s, err = json.Marshal(suite)
				if err != nil {
					return
				}
				fmt.Println(string(s))
				return
			},
			OnTest: func(cases []tapj.CaseEvent, test tapj.TestEvent) (err error) {
				prefixMatches := true
				for i, kase := range cases {
					if prefixMatches && i < len(currentCases) && kase == currentCases[i] {
						continue
					} else {
						prefixMatches = false
					}

					var s []byte
					s, err = json.Marshal(kase)
					if err != nil {
						return
					}
					fmt.Println(string(s))
				}
				currentCases = cases

				var s []byte
				s, err = json.Marshal(test)
				if err != nil {
					return
				}
				fmt.Println(string(s))
				return
			},
			OnFinal: func(suite tapj.SuiteEvent, final tapj.FinalEvent) (err error) {
				var s []byte
				s, err = json.Marshal(final)
				if err != nil {
					return
				}
				fmt.Println(string(s))
				return
			},
		}
	case "pretty":
		pretty := &reporters.Pretty{Writer: os.Stdout, Jobs: *jobs}
		runDecodingCallbacks = &tapj.DecodingCallbacks{
			OnSuite: pretty.SuiteStarted,
			OnTest:  pretty.TestFinished,
			OnFinal: pretty.SuiteFinished,
		}
	default:
		fmt.Fprintln(os.Stderr, "Unknown format", *format)
		os.Exit(254)
	}

	seed := int(rand.Int31())
	testRunners, err := runner.EnumerateTestRunners(seed)

	// First enumerate, accumulating suite, files and filters for each test to run
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if len(exitError.Stderr) > 0 {
				fmt.Fprintln(os.Stderr, string(exitError.Stderr))
			}

			waitStatus := exitError.Sys().(syscall.WaitStatus)
			os.Exit(waitStatus.ExitStatus())
		}

		fmt.Fprintln(os.Stderr, "Test runner enumeration failed.")
		os.Exit(1)
	}

	final, err := parallelTestRun(runDecodingCallbacks, seed,
		testRunners, *jobs)

	if !final.Passed() {
		os.Exit(1)
	}
}
