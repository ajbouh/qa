package run

import (
	"flag"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"qa/cmd"
	"qa/fileevents"
	"qa/run"
	"qa/runner"
	"qa/tapjio"
	"qa/watch"
	"sync"
	"syscall"
)

func trapSignals(closers []io.Closer, stdin io.Reader, stderr io.Writer) func() {
	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for signal
		_, ok := <-c
		if ok {
			for _, closer := range closers {
				defer closer.Close()
			}
		}
	}(sigc)

	if stdin != nil {
		go func() {
			for _, closer := range closers {
				defer closer.Close()
			}
			io.Copy(ioutil.Discard, stdin)
		}()
	}

	return func() {
		defer signal.Stop(sigc)
		defer close(sigc)
	}
}

func Watch(watches []*watch.Watch) error {
	runEnvChan := make(chan *run.Env)

	wg := &sync.WaitGroup{}
	for _, w := range watches {
		wg.Add(1)
		go func(w *watch.Watch) {
			defer wg.Done()
			w.ProcessSubscriptionEvents(runEnvChan)
		}(w)

		w.WriteStatus()
	}

	go func() {
		wg.Wait()
		close(runEnvChan)
	}()

	// Try to run each runEnv sequentially.
	for runEnv := range runEnvChan {
		_, err := run.Run(runEnv)
		if err != nil {
			return err
		}

		for _, w := range watches {
			w.WriteStatus()
		}
	}

	return nil
}

func gogogo(cmdEnv *cmd.Env, runEnv *run.Env, shouldWatch bool) error {
	srv := runEnv.Server
	defer srv.Close()

	closers := []io.Closer{
		srv,
	}

	var err error
	var watcher fileevents.Watcher
	var watches []*watch.Watch

	if shouldWatch {
		watcher, err = fileevents.StartWatchman("/tmp/watchman")
		if err != nil {
			return err
		}
		defer watcher.Close()

		closers = append(closers, watcher)

		for _, runnerConfig := range runEnv.RunnerConfigs {
			dir, expr, err := watch.RunnerConfigToWatchExpression(runnerConfig, []tapjio.FilePath{})
			if err != nil {
				return nil
			}

			sub, err := watcher.Subscribe(dir, "tests", expr)
			if err != nil {
				return nil
			}
			defer sub.Close()
			closers = append(closers, sub)

			w := watch.NewWatch(cmdEnv.Stderr, dir, runEnv, sub, runnerConfig)
			watches = append(watches, w)
		}
	}

	cleanupTrap := trapSignals(closers, cmdEnv.Stdin, cmdEnv.Stderr)
	defer cleanupTrap()

	if watcher != nil {
		return Watch(watches)
	}

	passed, err := run.Run(runEnv)
	if err != nil {
		return err
	}

	if passed {
		return nil
	}

	return &cmd.QuietError{1}
}

func Main(env *cmd.Env, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)

	f := DefineFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}

	f.ApplyImpliedDefaults()
	runnerConfigs := f.ParseRunnerConfigs(env, flags.Args())
	runEnv, err := f.NewEnv(env, runnerConfigs)
	if err != nil {
		return err
	}

	return gogogo(env, runEnv, f.Watch())
}

func Framework(frameworkName string, env *cmd.Env, args []string) error {
	flags := flag.NewFlagSet(frameworkName, flag.ContinueOnError)

	f := DefineFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}

	f.ApplyImpliedDefaults()

	runnerArgs := flags.Args()
	if len(runnerArgs) == 0 {
		runnerArgs = []string{run.DefaultGlob(frameworkName)}
	}
	runEnv, err := f.NewEnv(env, []runner.Config{
		f.NewRunnerConfig(env, frameworkName, runnerArgs),
	})

	if err != nil {
		return err
	}

	return gogogo(env, runEnv, f.Watch())
}
