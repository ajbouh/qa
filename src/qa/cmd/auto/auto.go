package auto

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"qa/cmd"
	"qa/cmd/run"
	"qa/fileevents"
	"qa/runner"
	"syscall"
)

// When watchman emits an event, execute a qa run for that test
// Expect:
// {
//   "version":   "1.6",
//   "subscribe": "mysubscriptionname"
// }
type eventFileLister fileevents.Event

func (s eventFileLister) Dir() string {
	return s.Root
}

func (s eventFileLister) Patterns() []string {
	var names []string
	for _, file := range s.Files {
		names = append(names, file.Name)
	}
	return names
}

func (s eventFileLister) ListFiles() ([]string, error) {
	return s.Patterns(), nil
}

func pruneRunEnvForFiles(
	runEnv *run.Env,
	runnerConfig runner.Config,
	event *fileevents.Event) *run.Env {

	pruned := *runEnv
	runnerConfig.FileLister = eventFileLister(*event)
	pruned.RunnerConfigs = []runner.Config{
		runnerConfig,
	}

	return &pruned
}

func subscribeToRunnerConfigFiles(watcher fileevents.Watcher, runnerConfig runner.Config) (*fileevents.Subscription, error) {
	dir, err := filepath.Abs(runnerConfig.FileLister.Dir())
	if err != nil {
		return nil, err
	}

	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}

	expr := map[string](interface{}){
		"expression": []string{"match", runnerConfig.FileLister.Patterns()[0], "wholename"},
		"fields":     []string{"name", "new", "exists"},
		"defer_vcs":  true,
	}

	return watcher.Subscribe(dir, "tests", expr)
}

func Main(env *cmd.Env, args []string) error {
	watcher, err := fileevents.StartWatchman("/tmp/watchman")
	if err != nil {
		return err
	}
	defer watcher.Close()

	flags := flag.NewFlagSet("auto", flag.ContinueOnError)

	runFlags := run.DefineFlags(flags)
	err = flags.Parse(args)
	if err != nil {
		return err
	}

	runEnv, err := runFlags.NewEnv(env, flags.Args())
	if err != nil {
		return err
	}

	srv := runEnv.Server
	defer srv.Close()
	go srv.Run()

	runEnvChan := make(chan *run.Env)

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for signal
		sig, ok := <-c
		if ok {
			fmt.Fprintln(env.Stderr, "Got signal:", sig)
			srv.Close()
			watcher.Close()
			close(runEnvChan)
		}
	}(sigc)
	defer signal.Stop(sigc)
	defer close(sigc)

	for _, runnerConfig := range runEnv.RunnerConfigs {
		sub, err := subscribeToRunnerConfigFiles(watcher, runnerConfig)
		if err != nil {
			return nil
		}
		defer sub.Close()

		go func(sub *fileevents.Subscription, runnerConfig runner.Config) {
			for event := range sub.Events {
				runEnvChan <- pruneRunEnvForFiles(runEnv, runnerConfig, event)
			}
		}(sub, runnerConfig)
	}

	// Figure out which file changed. If file that changed matches the existing glob,
	// start running that test.
	for runEnv := range runEnvChan {
		_, err := run.Run(runEnv)
		if err != nil {
			return err
		}
	}

	return nil
}
