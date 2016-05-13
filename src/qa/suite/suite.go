package suite

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"qa/runner"
	"qa/runner/server"
	"qa/tapjio"
)

type testResult struct {
	testEvent tapjio.TestEvent
	testError error
}

type testSuiteRunner struct {
	seed    int
	runners []runner.TestRunner
	count   int
	srv     *server.Server
}

func NewTestSuiteRunner(seed int,
	srv *server.Server,
	runners []runner.TestRunner) *testSuiteRunner {

	count := 0
	for _, runner := range runners {
		count += runner.TestCount()
	}

	return &testSuiteRunner{
		seed:    seed,
		runners: runners,
		count:   count,
		srv:     srv,
	}
}

func (self *testSuiteRunner) Run(
	maxJobs int,
	visitor tapjio.Visitor) (final tapjio.FinalEvent, err error) {
	startTime := time.Now().UTC()

	suite := tapjio.NewSuiteEvent(startTime, self.count, self.seed)
	final = *tapjio.NewFinalEvent(suite)

	err = visitor.SuiteStarted(*suite)
	if err != nil {
		return
	}

	defer func() {
		final.Time = time.Now().UTC().Sub(startTime).Seconds()

		finalErr := visitor.SuiteFinished(final)
		if err == nil {
			err = finalErr
		}
	}()

	var testRunnerChan = make(chan runner.TestRunner, maxJobs)

	go func() {
		runner.By(func(r1, r2 *runner.TestRunner) bool { return (*r2).TestCount() < (*r1).TestCount() }).Sort(self.runners)

		for _, testRunner := range self.runners {
			testRunnerChan <- testRunner
		}
		close(testRunnerChan)
	}()

	var abort = false
	var traceChan = make(chan tapjio.TraceEvent, maxJobs)
	var testResultChan = make(chan testResult, maxJobs)
	var testStartChan = make(chan tapjio.TestStartedEvent, maxJobs)

	var awaitJobs sync.WaitGroup
	awaitJobs.Add(maxJobs)

	for i := 0; i < maxJobs; i++ {
		i := i
		go func() {
			defer awaitJobs.Done()
			for testRunner := range testRunnerChan {
				if abort {
					for i := testRunner.TestCount(); i > 0; i-- {
						testResultChan <- testResult{testError: errors.New("already aborted")}
					}
				} else {
					var awaitRun sync.WaitGroup
					awaitRun.Add(1)
					testRunner.Run(
						map[string]string{"QA_WORKER": fmt.Sprintf("%d", i)},
						tapjio.DecodingCallbacks{
							OnTestBegin: func(test tapjio.TestStartedEvent) error {
								testStartChan <- test
								return nil
							},
							OnTest: func(test tapjio.TestEvent) error {
								testResultChan <- testResult{test, nil}
								return nil
							},
							OnTrace: func(trace tapjio.TraceEvent) error {
								traceChan <- trace
								return nil
							},
							OnFinal: func(final tapjio.FinalEvent) error {
								awaitRun.Done()
								return nil
							},
						})
					awaitRun.Wait()
				}
			}
		}()
	}

	go func() {
		awaitJobs.Wait()
		close(traceChan)
	}()

	for traceChan != nil {
		select {
		case trace, ok := <-traceChan:
			if !ok {
				traceChan = nil
				continue
			}

			err = visitor.TraceEvent(trace)
			if err != nil {
				return
			}
		case testStart := <-testStartChan:
			err = visitor.TestStarted(testStart)
			if err != nil {
				return
			}
		case testResult := <-testResultChan:
			err = testResult.testError
			if err == nil {
				test := testResult.testEvent
				(&final.Counts).Increment(test.Status)

				err = visitor.TestFinished(test)
				if err != nil {
					return
				}
			}

			if err != nil {
				abort = true
				(&final.Counts).Increment(tapjio.Error)
				fmt.Fprintln(os.Stderr, "Error:", err)
				return
			}
		}
	}

	return
}
