package ruby

import (
	"errors"
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

type ContextConfig struct {
	Seed              int
	Rubylib           []string
  RunnerAssetName   string
	TraceProbes       []string
	SquashPolicy      SquashPolicy
	PassthroughConfig map[string](interface{})
}

type context struct {
	requestCh    chan interface{}
	server       *server.Server
	process      *os.Process
	config       *ContextConfig
}

func StartContext(cfg *ContextConfig, server *server.Server, workerEnvs []map[string]string, files []string) (*context, error) {
	requestCh := make(chan interface{}, 1)

	sharedData, err := assets.Asset("ruby/shared.rb")
	if err != nil {
		return nil, err
	}
	var sharedCode = string(sharedData)

	runnerData, err := assets.Asset(cfg.RunnerAssetName)
	if err != nil {
		return nil, err
	}
	var runnerCode = string(runnerData)

	args := []string{}
	for _, lib := range cfg.Rubylib {
		args = append(args, "-I", lib)
	}
	args = append(args,
		"-e", sharedCode,
		"-e", runnerCode,
		"--",
		server.ExposeChannel(requestCh))

	// First request is a list of worker environments and list of all test files to require.
	requestCh <- map[string](interface{}){
		"workerEnvs": workerEnvs,
		"files": files,
		"passthrough": cfg.PassthroughConfig,
	}

	for _, traceProbe := range cfg.TraceProbes {
		args = append(args, "--trace-probe", traceProbe)
	}

	cmd := exec.Command("ruby", args...)

	// The code below will wrap the worker in a gdb session.
	// args = append([]string{"-ex=set follow-fork-mode child", "-ex=r", "--args", "ruby"}, args...)
	// cmd := exec.Command("gdb", args...)

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	return &context{
		requestCh: requestCh,
		server: server,
		config: cfg,
		process: cmd.Process,
	}, nil
}

// TODO(adamb) Should also cancel all existing waitgroups
func (self *context) Close() (err error) {
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

func (self *context) TraceProbes() []string {
	return self.config.TraceProbes
}

func (self *context) EnumerateTests() (traceEvents []tapjio.TraceEvent, testRunners []runner.TestRunner, err error) {
	var wg sync.WaitGroup
	wg.Add(1)

	cfg := self.config
	var currentRunner *rubyRunner
	serverAddress := self.server.Decode(&tapjio.DecodingCallbacks{
		OnTrace: func(trace tapjio.TraceEvent) (err error) {
			traceEvents = append(traceEvents, trace)
			return
		},
		OnTest: func(test tapjio.TestEvent) (err error) {
			if cfg.SquashPolicy == SquashNothing ||
				cfg.SquashPolicy == SquashByFile && (currentRunner == nil || currentRunner.file != test.File) ||
				cfg.SquashPolicy == SquashAll && currentRunner == nil {
				if currentRunner != nil {
					testRunners = append(testRunners, *currentRunner)
				}
				currentRunner = &rubyRunner{
					ctx: self,
					file: test.File,
					filters: []string{},
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
			"--seed", fmt.Sprintf("%v", cfg.Seed),
			"--tapj-sink", serverAddress,
		})
	wg.Wait()

	return
}

func (self *context) subscribeVisitor(visitor tapjio.Visitor) string {
	return self.server.Decode(visitor)
}

func (self *context) request(env map[string]string, args []string) {
	r := []interface{}{
		env,
		args,
	}
	self.requestCh <- r
}

type rubyRunner struct {
	ctx *context
	file    string
	filters []string
}

func (self rubyRunner) TestCount() int {
	return len(self.filters)
}

// debitFilter returns a new slice, with the given string removed from the given slice. Returns
// an error if the given string is not present.
func debitFilter(filters []string, filter string) ([]string, error) {
	for i, f := range filters {
		if filter == f {
			return append(filters[:i], filters[i+1:]...), nil
		}
	}

	return filters, errors.New(
		fmt.Sprintf("Unexpected test filter: %s. Expected one of %v", filter, filters))
}

// Run executes the rubyRunner's tests with the given environment variables. Events triggered
// by the run will be invoked on the given callbacks instance. Returns an error if anything
// goes wrong before starting the tests or while processing the a test event.
// NOTE(adamb) It is not careful about ensuring the test is no longer running in the case of an
//     error.
func (self rubyRunner) Run(env map[string]string, callbacks tapjio.DecodingCallbacks) error {
	var allowedBeginFilters, allowedFinishFilters []string
	allowedBeginFilters = append(allowedBeginFilters, self.filters...)
	allowedFinishFilters = append(allowedFinishFilters, self.filters...)
	var wg sync.WaitGroup
	wg.Add(1)
	onTestBegin := callbacks.OnTestBegin
	callbacks.OnTestBegin = func(event tapjio.TestStartedEvent) error {
		var err error
		allowedBeginFilters, err = debitFilter(allowedBeginFilters, event.Filter)
		if err != nil {
			return err
		}

		if onTestBegin == nil {
			return nil
		}
		return onTestBegin(event)
	}
	onTest := callbacks.OnTest
	callbacks.OnTest = func(event tapjio.TestEvent) error {
		var err error
		allowedFinishFilters, err = debitFilter(allowedFinishFilters, event.Filter)
		if err != nil {
			return err
		}

		if onTest == nil {
			return nil
		}
		return onTest(event)
	}

	onFinal := callbacks.OnFinal
	callbacks.OnFinal = func(final tapjio.FinalEvent) error {
		if onFinal == nil {
			return nil
		}

		err := onFinal(final)
		wg.Done()
		return err
	}

	self.ctx.request(
		env,
		append([]string{
			"--seed", fmt.Sprintf("%v", self.ctx.config.Seed),
			"--tapj-sink", self.ctx.subscribeVisitor(&callbacks),
		}, self.filters...))

	wg.Wait()
	return nil
}