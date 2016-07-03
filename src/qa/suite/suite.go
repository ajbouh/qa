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

type eventUnion struct {
	trace  *tapjio.TraceEvent
	begin  *tapjio.TestStartedEvent
	finish *tapjio.TestEvent
	error  error
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
	workerEnvs []map[string]string,
	visitor tapjio.Visitor) (final tapjio.FinalEvent, err error) {

	numWorkers := len(workerEnvs)
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

	var testRunnerChan = make(chan runner.TestRunner, numWorkers)

	// Enqueue each testRunner on testRunnerChan
	go func() {
		// Sort runners by test count. This heuristic helps our workers avoid being idle
		// near the end of the run by running testRunners with the most tests first, avoiding
		// scenarios where the last testRunner we run has many tests, causing the entire test
		// run to drag on needlessly while other workers are idle.
		runner.By(func(r1, r2 *runner.TestRunner) bool { return (*r2).TestCount() < (*r1).TestCount() }).Sort(self.runners)

		for _, testRunner := range self.runners {
			testRunnerChan <- testRunner
		}
		close(testRunnerChan)
	}()

	var abort = false
	var eventChan = make(chan eventUnion, numWorkers)

	var awaitJobs sync.WaitGroup
	awaitJobs.Add(numWorkers)

	for _, workerEnv := range workerEnvs {
		env := workerEnv
		go func() {
			defer awaitJobs.Done()
			for testRunner := range testRunnerChan {
				if abort {
					for i := testRunner.TestCount(); i > 0; i-- {
						eventChan <- eventUnion{error: errors.New("already aborted")}
					}
				} else {
					var awaitRun sync.WaitGroup
					awaitRun.Add(1)
					testRunner.Run(
						env,
						tapjio.DecodingCallbacks{
							OnTestBegin: func(test tapjio.TestStartedEvent) error {
								eventChan <- eventUnion{nil, &test, nil, nil}
								return nil
							},
							OnTest: func(test tapjio.TestEvent) error {
								eventChan <- eventUnion{nil, nil, &test, nil}
								return nil
							},
							OnTrace: func(trace tapjio.TraceEvent) error {
								eventChan <- eventUnion{&trace, nil, nil, nil}
								return nil
							},
							OnEnd: func(reason error) error {
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
		close(eventChan)
	}()

	for eventUnion := range eventChan {
		if eventUnion.trace != nil {
			err = visitor.TraceEvent(*eventUnion.trace)
			if err != nil {
				return
			}
			continue
		}

		begin := eventUnion.begin
		if begin != nil {
			err = visitor.TestStarted(*begin)
			if err != nil {
				return
			}
			continue
		}

		err = eventUnion.error
		if err == nil {
			test := eventUnion.finish
			final.Counts.Increment(test.Status)

			err = visitor.TestFinished(*test)
			if err != nil {
				return
			}
			continue
		}

		if err != nil {
			abort = true
			final.Counts.Increment(tapjio.Error)
			fmt.Fprintln(os.Stderr, "Error:", err)
			return
		}
	}

	return
}
