package run

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"qa/cmd"
	"qa/runner"
	"qa/runner/ruby"
	"qa/runner/server"
	"qa/suite"
	"qa/tapjio"
)

type Env struct {
	Seed          int
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

	var testRunners []runner.TestRunner
	for _, runnerConfig := range env.RunnerConfigs {
		starter, ok := starters[runnerConfig.Name]
		if !ok {
			return final, errors.New("Could not find starter: " + runnerConfig.Name)
		}

		ctx, err := starter(env.Server, env.WorkerEnvs, runnerConfig)
		defer ctx.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error! %v\n", err)
			return final, err
		}

		traceEvents, runners, err := ctx.EnumerateRunners()
		if err != nil {
			return final, err
		}

		testRunners = append(testRunners, runners...)

		visitor := env.Visitor
		for _, traceEvent := range traceEvents {
			err := visitor.TraceEvent(traceEvent)
			if err != nil {
				return final, err
			}
		}
	}

	return suite.Run(env.Visitor, env.WorkerEnvs, env.Seed, testRunners)
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
