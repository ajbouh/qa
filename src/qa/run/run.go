package run

import (
	"errors"
	"log"
	"os"
	"qa/runner"
	"qa/runner/ruby"
	"qa/runner/server"
	"qa/tapjio"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"time"
)

type Env struct {
	SeedFn            func(repetition int) int
	SuiteLabel        string
	SuiteCoderef      string
	WorkerEnvs        []map[string]string
	RunnerConfigs     []runner.Config
	Visitor           tapjio.Visitor
	Server            *server.Server
	TestRunnerVisitor func(testRunner runner.TestRunner, lastRunner bool) error
	Memprofile        string
	Heapdump          string
	Runs              int
}

var defaultGlobs = map[string]string{
	"rspec":     "spec/**/*spec.rb",
	"minitest":  "test/**/test*.rb",
	"test-unit": "test/**/test*.rb",
}

func DefaultGlob(runner string) string {
	return defaultGlobs[runner]
}

type contextStarter func(
	srv *server.Server,
	workerEnvs []map[string]string,
	runnerConfig runner.Config) (runner.Context, error)

func rubyContextStarter(runnerAssetName string) contextStarter {
	return func(
		srv *server.Server,
		workerEnvs []map[string]string,
		runnerConfig runner.Config) (runner.Context, error) {

		config := &ruby.ContextConfig{
			RunnerConfig:    runnerConfig,
			Rubylib:         []string{"spec", "lib", "test"},
			RunnerAssetName: runnerAssetName,
		}

		ctx, err := ruby.StartContext(srv, workerEnvs, config)
		if err != nil {
			return nil, err
		}

		return ctx, nil
	}
}

var starters = map[string]contextStarter{
	"rspec":     rubyContextStarter("ruby/rspec.rb"),
	"minitest":  rubyContextStarter("ruby/minitest.rb"),
	"test-unit": rubyContextStarter("ruby/test-unit.rb"),
}

func Run(env *Env) (bool, error) {
	startTime := time.Now().UTC()
	visitor := env.Visitor

	var testRunners []runner.TestRunner
	count := 0
	for _, runnerConfig := range env.RunnerConfigs {
		starter, ok := starters[runnerConfig.Name]
		if !ok {
			return false, errors.New("Could not find starter: " + runnerConfig.Name)
		}

		ctx, err := starter(env.Server, env.WorkerEnvs, runnerConfig)
		if err != nil {
			return false, err
		}
		defer ctx.Close()

		traceEvents, runners, err := ctx.EnumerateRunners(env.SeedFn(0))
		if err != nil {
			return false, err
		}

		testRunnerVisitor := env.TestRunnerVisitor
		lastRunnerIx := len(runners) - 1
		for ix, runner := range runners {
			count += runner.TestCount()

			if testRunnerVisitor != nil {
				err := testRunnerVisitor(runner, lastRunnerIx == ix)
				if err != nil {
					return false, err
				}
			}
		}

		testRunners = append(testRunners, runners...)

		for _, traceEvent := range traceEvents {
			err := visitor.TraceEvent(traceEvent)
			if err != nil {
				return false, err
			}
		}
	}

	var err error
	passed := true

	for runNo := 1; runNo <= env.Runs; runNo++ {
		if runNo > 1 {
			startTime = time.Now().UTC()
		}

		seed := env.SeedFn(runNo)
		suiteEvent := tapjio.NewSuiteBeginEvent(startTime, count, seed)
		suiteEvent.Label = env.SuiteLabel
		suiteEvent.Coderef = env.SuiteCoderef
		err = visitor.SuiteBegin(*suiteEvent)
		if err != nil {
			return false, visitEnd(visitor, err)
		}

		final := *tapjio.NewSuiteFinishEvent(suiteEvent)
		err = runner.RunAll(visitor, env.WorkerEnvs, final.Counts, seed, testRunners)
		if !final.Passed() {
			passed = false
		}

		if err != nil {
			break
		}

		final.Time = time.Now().UTC().Sub(startTime).Seconds()

		finalErr := visitor.SuiteFinish(final)
		if err == nil {
			err = finalErr
		}

		if err != nil {
			break
		}
	}

	if env.Memprofile != "" {
		f, err := os.Create(env.Memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
		f.Close()
	}

	if env.Heapdump != "" {
		f, err := os.Create(env.Heapdump)
		if err != nil {
			log.Fatal("could not create heap dump: ", err)
		}
		debug.WriteHeapDump(f.Fd())
		f.Close()
	}

	if err != nil {
		return passed, visitEnd(visitor, err)
	}

	return passed, visitEnd(visitor, nil)
}

func visitEnd(visitor tapjio.Visitor, reason error) error {
	err := visitor.End(reason)
	if reason == nil {
		return err
	}

	return reason
}
