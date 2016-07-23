package run

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"qa/cmd"
	"qa/emitter"
	"qa/runner"
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

func Run(env *Env) (tapjio.FinalEvent, error) {
	var final tapjio.FinalEvent

	var testRunners []runner.TestRunner
	for _, runnerConfig := range env.RunnerConfigs {
		em, err := emitter.Resolve(env.Server, env.WorkerEnvs, runnerConfig)
		defer em.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error! %v\n", err)
			return final, err
		}

		traceEvents, runners, err := em.EnumerateTests()
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
