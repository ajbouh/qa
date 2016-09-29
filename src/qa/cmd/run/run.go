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
		defer close(runEnvChan)
		wg.Wait()
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

	var trappedStdin io.Reader
	if shouldWatch {
		trappedStdin = cmdEnv.Stdin
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

	cleanupTrap := trapSignals(closers, trappedStdin, cmdEnv.Stderr)
	defer cleanupTrap()

	if watcher != nil {
		return Watch(watches)
	}

	passed, err := run.Run(runEnv)
	if err != nil {
		if quietError, ok := err.(*cmd.QuietError); ok {
			if quietError.Status == 0 {
				return nil
			}
		}

		return err
	}

	if passed {
		return nil
	}

	return &cmd.QuietError{1}
}

func Main(env *cmd.Env, argv []string) error {
	flags := flag.NewFlagSet(argv[0], flag.ContinueOnError)

	f := DefineFlags(env.Vars, flags)
	err := flags.Parse(argv[1:])
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

func FrameworkWithVisitor(frameworkName string, env *cmd.Env, argv []string, visitor tapjio.Visitor) error {
	flags := flag.NewFlagSet(argv[0], flag.ContinueOnError)

	f := DefineFlags(env.Vars, flags)
	err := flags.Parse(argv[1:])
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

	if visitor != nil {
		runEnv.Visitor = tapjio.MultiVisitor([]tapjio.Visitor{visitor, runEnv.Visitor})
	}

	if err != nil {
		return err
	}

	return gogogo(env, runEnv, f.Watch())
}

func Framework(frameworkName string, env *cmd.Env, argv []string) error {
  return FrameworkWithVisitor(frameworkName, env, argv, nil)
}
