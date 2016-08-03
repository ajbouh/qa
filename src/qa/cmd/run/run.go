package run

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"qa/cmd"
	"qa/runner"
	"qa/runner/ruby"
	"qa/runner/server"
	"qa/tapjio"
)

type Env struct {
	Seed          int
	SuiteLabel    string
	SuiteCoderef  string
	WorkerEnvs    []map[string]string
	RunnerConfigs []runner.Config
	Visitor       tapjio.Visitor
	Server        *server.Server
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

func Run(env *Env) (tapjio.FinalEvent, error) {
	var final tapjio.FinalEvent
	startTime := time.Now().UTC()
	visitor := env.Visitor

	var testRunners []runner.TestRunner
	count := 0
	for _, runnerConfig := range env.RunnerConfigs {
		starter, ok := starters[runnerConfig.Name]
		if !ok {
			return final, errors.New("Could not find starter: " + runnerConfig.Name)
		}

		ctx, err := starter(env.Server, env.WorkerEnvs, runnerConfig)
		if err != nil {
			return final, err
		}
		defer ctx.Close()

		traceEvents, runners, err := ctx.EnumerateRunners()
		if err != nil {
			return final, err
		}

		for _, runner := range runners {
			count += runner.TestCount()
		}

		testRunners = append(testRunners, runners...)

		for _, traceEvent := range traceEvents {
			err := visitor.TraceEvent(traceEvent)
			if err != nil {
				return final, err
			}
		}
	}

	suiteEvent := tapjio.NewSuiteEvent(startTime, count, env.Seed)
	suiteEvent.Label = env.SuiteLabel
	suiteEvent.Coderef = env.SuiteCoderef
	err := visitor.SuiteStarted(*suiteEvent)
	if err != nil {
		return final, err
	}

	final = *tapjio.NewFinalEvent(suiteEvent)
	err = runner.RunAll(visitor, env.WorkerEnvs, final.Counts, testRunners)

	final.Time = time.Now().UTC().Sub(startTime).Seconds()

	finalErr := visitor.SuiteFinished(final)
	if err == nil {
		err = finalErr
	}

	return final, err
}

func Main(env *cmd.Env, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)

	f := DefineFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}

	runEnv, err := f.NewEnv(env, flags.Args())
	if err != nil {
		return err
	}

	srv := runEnv.Server
	defer srv.Close()
	go srv.Run()

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for signal
		sig, ok := <-c
		if ok {
			fmt.Fprintln(env.Stderr, "Got signal:", sig)
			srv.Close()
		}
	}(sigc)
	defer signal.Stop(sigc)
	defer close(sigc)

	var final tapjio.FinalEvent
	final, err = Run(runEnv)
	if err != nil {
		return err
	}

	if !final.Passed() {
		return errors.New("Test(s) failed.")
	}

	return nil
}
