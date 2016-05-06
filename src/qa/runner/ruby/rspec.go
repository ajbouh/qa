package ruby

import (
	"fmt"
	"os"
	"os/exec"
	"qa/runner"
	"qa/runner/assets"
	"qa/runner/server"
	"qa/tapjio"
	"sync"
)

type SquashPolicy int

const (
	SquashNothing SquashPolicy = iota
	SquashByFile
	SquashAll
)

type rspecContext struct {
	requestCh    chan interface{}
	seed         int
	server       *server.Server
	process      *os.Process
	squashPolicy SquashPolicy
}

func NewRspecContext(seed int, server *server.Server) *rspecContext {
	return &rspecContext{
		requestCh: make(chan interface{}),
		seed:      seed,
		server:    server,
	}
}

func (self *rspecContext) SquashPolicy(j SquashPolicy) {
	self.squashPolicy = j
}

func (self *rspecContext) Start(files []string) error {
	fmt.Fprintln(os.Stderr, "Starting rspec context")
	sharedData, err := assets.Asset("ruby/shared.rb")
	if err != nil {
		return err
	}
	var sharedCode = string(sharedData)

	rspecRunnerData, err := assets.Asset("ruby/rspec.rb")
	if err != nil {
		return err
	}
	var runnerCode = string(rspecRunnerData)

	args := append([]string{
		"-I", "lib", "-I", "spec",
		"-e", sharedCode,
		"-e", runnerCode,
		"--",
		self.server.ExposeChannel(self.requestCh),
	}, files...)
	cmd := exec.Command("ruby", args...)

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	err = cmd.Start()
	if err != nil {
		return err
	}

	self.process = cmd.Process
	fmt.Fprintln(os.Stderr, "Started rspec context", self.process)
	return nil
}

// TODO(adamb) Should also cancel all existing waitgroups
func (self *rspecContext) Close() (err error) {
	s := *self.server
	if self.process != nil {
		err = self.process.Kill()
	}

	closeErr := s.Close()
	if closeErr != nil {
		err = closeErr
	}

	return
}

func (self *rspecContext) EnumerateTests(seed int) (traceEvents []tapjio.TraceEvent, testRunners []runner.TestRunner, err error) {
	var wg sync.WaitGroup
	wg.Add(1)

	var currentRunner *rspecRunner
	serverAddress := self.server.Decode(&tapjio.DecodingCallbacks{
		OnTrace: func(trace tapjio.TraceEvent) (err error) {
			traceEvents = append(traceEvents, trace)
			return
		},
		OnTest: func(test tapjio.TestEvent) (err error) {
			if self.squashPolicy == SquashNothing ||
				self.squashPolicy == SquashByFile && (currentRunner == nil || currentRunner.file != test.File) ||
				self.squashPolicy == SquashAll && currentRunner == nil {
				if currentRunner != nil {
					testRunners = append(testRunners, *currentRunner)
				}
				currentRunner = &rspecRunner{
					context: self,
					file:    test.File,
					filters: []string{},
					seed:    seed,
				}
			}
			currentRunner.filters = append(currentRunner.filters, test.Filter)
			return
		},
		OnFinal: func(final tapjio.FinalEvent) (err error) {
			if currentRunner != nil {
				testRunners = append(testRunners, *currentRunner)
			}
			wg.Done()
			return
		},
	})

	self.request(
		map[string]string{},
		[]string{
			"--dry-run",
			"--seed", fmt.Sprintf("%v", self.seed),
			"--tapj-sink", serverAddress,
		})
	wg.Wait()

	return
}

func (self *rspecContext) subscribeVisitor(visitor tapjio.Visitor) string {
	return self.server.Decode(visitor)
}

func (self *rspecContext) request(env map[string]string, args []string) {
	r := []interface{}{
		env,
		args,
	}
	self.requestCh <- r
}

type rspecRunner struct {
	context *rspecContext
	seed    int
	file    string
	filters []string
}

func (self rspecRunner) TestCount() int {
	return len(self.filters)
}

func (self rspecRunner) Run(env map[string]string, callbacks tapjio.DecodingCallbacks) error {
	var wg sync.WaitGroup
	wg.Add(1)
	onFinal := callbacks.OnFinal
	callbacks.OnFinal = func(final tapjio.FinalEvent) error {
		if onFinal == nil {
			return nil
		}

		err := onFinal(final)
		wg.Done()
		return err
	}

	self.context.request(
		env,
		append([]string{
			"--seed", fmt.Sprintf("%v", self.seed),
			"--tapj-sink", self.context.subscribeVisitor(&callbacks),
		}, self.filters...))

	wg.Wait()
	return nil
}
