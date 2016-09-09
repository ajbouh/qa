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

type ContextConfig struct {
	RunnerConfig    runner.Config
	Rubylib         []string
	RunnerAssetName string
}

type context struct {
	requestCh chan interface{}
	srv       *server.Server
	addresses []string
	process   *os.Process
	config    *ContextConfig
	mutex     *sync.Mutex
}

func StartContext(srv *server.Server, workerEnvs []map[string]string, cfg *ContextConfig) (*context, error) {
	runnerCfg := cfg.RunnerConfig

	files, err := runnerCfg.Files()
	if err != nil {
		return nil, err
	}

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

	address, requestCh, requestErrChan, err := srv.ExposeChannel()
	if err != nil {
		return nil, err
	}

	args = append(args,
		"-e", sharedCode,
		"-e", runnerCode,
		"--",
		address)

	for _, traceProbe := range runnerCfg.TraceProbes {
		args = append(args, "--trace-probe", traceProbe)
	}

	cmd := exec.Command("ruby", args...)

	if len(runnerCfg.EnvVars) > 0 {
		baseEnv := os.Environ()
		for envVarName, envVarValue := range runnerCfg.EnvVars {
			baseEnv = append(baseEnv, fmt.Sprintf("%s=%s", envVarName, envVarValue))
		}
		cmd.Env = baseEnv
	}

	// The code below will wrap the worker in a gdb session.
	// args = append([]string{"-ex=set follow-fork-mode child", "-ex=r", "--args", "ruby"}, args...)
	// cmd := exec.Command("gdb", args...)

	cmd.Dir = runnerCfg.Dir
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	// First request is a list of worker environments and list of all test files to require.
	requestCh <- map[string](interface{}){
		"workerEnvs":  workerEnvs,
		"files":       files,
		"passthrough": runnerCfg.PassthroughConfig,
	}

	ctx := &context{
		requestCh: requestCh,
		addresses: []string{},
		srv:       srv,
		config:    cfg,
		process:   cmd.Process,
		mutex:     &sync.Mutex{},
	}

	go func() {
		_, ok := <-requestErrChan
		if ok {
			ctx.Close()
		}
	}()

	go func() {
		defer ctx.cleanupAfterProcessDone()
		cmd.Process.Wait()
	}()

	return ctx, nil
}

func (self *context) cleanupAfterProcessDone() {
	m := self.mutex
	m.Lock()
	self.process = nil
	m.Unlock()

	self.cancelAllVisitorSubscriptions()

	m.Lock()
	close(self.requestCh)
	self.requestCh = nil
	m.Unlock()
}

// TODO(adamb) Should also cancel all existing waitgroups
func (self *context) Close() error {
	m := self.mutex
	m.Lock()
	defer m.Unlock()

	if self.process != nil {
		err := self.process.Kill()
		if err != nil {
			return err
		}
	}

	return nil
}

func (self *context) EnumerateRunners() (traceEvents []tapjio.TraceEvent, testRunners []runner.TestRunner, err error) {
	cfg := self.config
	var currentRunner *rubyRunner
	serverAddress, errChan, err := self.subscribeVisitor(&tapjio.DecodingCallbacks{
		OnTrace: func(trace tapjio.TraceEvent) (err error) {
			traceEvents = append(traceEvents, trace)
			return
		},
		OnTestFinish: func(test tapjio.TestFinishEvent) error {
			squashPolicy := cfg.RunnerConfig.SquashPolicy
			if squashPolicy == runner.SquashNothing ||
				squashPolicy == runner.SquashByFile && (currentRunner == nil || currentRunner.file != test.File) ||
				squashPolicy == runner.SquashAll && currentRunner == nil {
				if currentRunner != nil {
					testRunners = append(testRunners, *currentRunner)
				}
				currentRunner = &rubyRunner{
					ctx:        self,
					file:       test.File,
					filters:    []tapjio.TestFilter{},
					depEntries: []runner.TestDependencyEntry{},
				}
			}
			currentRunner.filters = append(currentRunner.filters, test.Filter)
			if test.Dependencies != nil {
				depEntry := runner.TestDependencyEntry{
					Label:        tapjio.TestLabel(test.Label, test.Cases),
					File:         test.File,
					Filter:       test.Filter,
					Dependencies: *test.Dependencies,
				}
				currentRunner.depEntries = append(currentRunner.depEntries, depEntry)
			}
			return nil
		},
		OnEnd: func(reason error) error {
			if currentRunner != nil {
				testRunners = append(testRunners, *currentRunner)
			}
			return nil
		},
	})
	if err != nil {
		return
	}

	args := []string{
		"--dry-run",
		"--seed", fmt.Sprintf("%v", cfg.RunnerConfig.Seed),
		"--tapj-sink", serverAddress,
	}
	for _, filter := range cfg.RunnerConfig.Filters {
		args = append(args, filter.String())
	}

	if err = self.request(map[string]string{}, args); err != nil {
		self.srv.Cancel(serverAddress)
		return
	}

	err = <-errChan
	return
}

func (self *context) addAddress(address string) {
	m := self.mutex
	m.Lock()
	defer m.Unlock()

	self.addresses = append(self.addresses, address)
}

func (self *context) subscribeVisitor(visitor tapjio.Visitor) (string, chan error, error) {
	address, errChan, err := self.srv.Decode(visitor)
	if err != nil {
		return "", nil, err
	}

	self.addAddress(address)
	return address, errChan, nil
}

func (self *context) cancelAllVisitorSubscriptions() {
	m := self.mutex
	m.Lock()
	addresses := append([]string{}, self.addresses...)
	self.addresses = []string{}
	m.Unlock()

	for _, address := range addresses {
		self.srv.Cancel(address)
	}
}

func (self *context) request(env map[string]string, args []string) error {
	m := self.mutex
	m.Lock()
	defer m.Unlock()
	if self.requestCh != nil {
		r := []interface{}{
			env,
			args,
		}
		self.requestCh <- r
		return nil
	} else {
		return errors.New("Already closed")
	}
}

type rubyRunner struct {
	ctx        *context
	file       tapjio.FilePath
	filters    []tapjio.TestFilter
	depEntries []runner.TestDependencyEntry
}

func (self rubyRunner) TestCount() int {
	return len(self.filters)
}

// debitFilter returns a new slice, with the given string removed from the given slice. Returns
// an error if the given string is not present.
func debitFilter(filters []tapjio.TestFilter, filter tapjio.TestFilter, kind string, saw []tapjio.TestFilter) ([]tapjio.TestFilter, error) {
	for i, f := range filters {
		if filter == f {
			return append(filters[:i], filters[i+1:]...), nil
		}
	}

	return filters, errors.New(
		fmt.Sprintf("Unexpected %s test filter: %s. Expected one of %v. Already saw %v", kind, filter, filters, saw))
}

func (self rubyRunner) Dependencies() []runner.TestDependencyEntry {
	return self.depEntries
}

// Run executes the rubyRunner's tests with the given environment variables. Events triggered
// by the run will be invoked on the given callbacks instance. Returns an error if anything
// goes wrong before starting the tests or while processing the a test event.
// NOTE(adamb) It is not careful about ensuring the test is no longer running in the case of an
//     error.
func (self rubyRunner) Run(env map[string]string, visitor tapjio.Visitor) error {
	var allowedBeginFilters, allowedFinishFilters []tapjio.TestFilter
	allowedBeginFilters = append(allowedBeginFilters, self.filters...)
	sawBeginFilters := []tapjio.TestFilter{}
	allowedFinishFilters = append(allowedFinishFilters, self.filters...)
	sawFinishFilters := []tapjio.TestFilter{}

	bothVisitors := tapjio.MultiVisitor(
		[]tapjio.Visitor{
			&tapjio.DecodingCallbacks{
				OnTestBegin: func(event tapjio.TestBeginEvent) error {
					var err error
					allowedBeginFilters, err = debitFilter(allowedBeginFilters, event.Filter, "begin", sawBeginFilters)
					sawBeginFilters = append(sawBeginFilters, event.Filter)
					return err
				},
				OnTestFinish: func(event tapjio.TestFinishEvent) error {
					var err error
					allowedFinishFilters, err = debitFilter(allowedFinishFilters, event.Filter, "finish", sawFinishFilters)
					sawFinishFilters = append(sawFinishFilters, event.Filter)
					return err
				},
				OnEnd: func(reason error) error {
					if reason != nil {
						return nil
					}

					if len(allowedFinishFilters) != 0 {
						return fmt.Errorf("Runner finished without emitting all expected tests. Never saw: %v. Did see: finish %v, begin %v", allowedFinishFilters, sawFinishFilters, sawBeginFilters)
					}

					return nil
				},
			},
			visitor,
		})

	address, errChan, err := self.ctx.subscribeVisitor(bothVisitors)
	if err != nil {
		return err
	}

	filterArgs := make([]string, len(self.filters))
	for ix, filter := range self.filters {
		filterArgs[ix] = string(filter)
	}

	if err = self.ctx.request(
		env,
		append([]string{
			"--seed", fmt.Sprintf("%v", self.ctx.config.RunnerConfig.Seed),
			"--tapj-sink", address,
		}, filterArgs...)); err != nil {
		self.ctx.srv.Cancel(address)
		return err
	}

	return <-errChan
}
