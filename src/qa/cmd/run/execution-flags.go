package run

import (
	"errors"
	"flag"
	"fmt"
	"runtime"
	"strings"

	"qa/cmd"
	"qa/run"
	"qa/runner"
	"qa/runner/server"
)

type executionFlags struct {
	jobs                *int
	squashPolicy        *runner.SquashPolicy
	listenNetwork       *string
	listenAddress       *string
	errorsCaptureLocals *bool
	captureStandardFds  *bool
	evalBeforeFork      *string
	evalAfterFork       *string
	sampleStack         *bool
	warmup              *bool
	eagerLoad           *bool
	seed                *int
}

type squashPolicyValue struct {
	value *runner.SquashPolicy
}

func (v *squashPolicyValue) String() string {
	if v.value == nil {
		return ""
	}

	switch *v.value {
	case runner.SquashAll:
		return "all"
	case runner.SquashNothing:
		return "nothing"
	case runner.SquashByFile:
		return "file"
	}

	return ""
}

func (v *squashPolicyValue) Set(s string) error {
	switch s {
	case "all":
		*v.value = runner.SquashAll
	case "none":
		*v.value = runner.SquashNothing
	case "file":
		*v.value = runner.SquashByFile
	default:
		return errors.New("Invalid squash policy: " + s)
	}

	return nil
}

func defineExecutionFlags(flags *flag.FlagSet) *executionFlags {
	squashPolicyValue := &squashPolicyValue{new(runner.SquashPolicy)}
	*squashPolicyValue.value = runner.SquashByFile
	flags.Var(squashPolicyValue, "squash", "One of: all, none, file")

	errorsCaptureLocalsDefault := false
	if runtime.GOOS == "darwin" {
		errorsCaptureLocalsDefault = true
	}

	return &executionFlags{
		seed:                flags.Int("seed", -1, "Set seed to use"),
		jobs:                flags.Int("jobs", runtime.NumCPU(), "Set number of jobs"),
		squashPolicy:        squashPolicyValue.value,
		listenNetwork:       flags.String("listen-network", "unix", "Specify unix or tcp socket for worker coordination"),
		listenAddress:       flags.String("listen-address", "/tmp/qa", "Listen address for worker coordination"),
		errorsCaptureLocals: flags.Bool("errors-capture-locals", errorsCaptureLocalsDefault, "Use runtime debug API to capture local variables when raising errors, disabled by default on some platforms"),
		captureStandardFds:  flags.Bool("capture-standard-fds", true, "Capture stdout and stderr"),
		evalBeforeFork:      flags.String("eval-before-fork", "", "Execute the given code before forking any workers or loading any files"),
		evalAfterFork:       flags.String("eval-after-fork", "", "Execute the given code after a worker forks, but before work begins"),
		sampleStack:         flags.Bool("sample-stack", false, "Enable stack sampling"),
		warmup:              flags.Bool("warmup", true, "Use a variety of experimental heuristics to warm up worker caches"),
		eagerLoad:           flags.Bool("eager-load", false, "Use a variety of experimental heuristics to eager load code"),
	}
}

func (f *executionFlags) Listen() (*server.Server, error) {
	return server.Listen(*f.listenNetwork, *f.listenAddress)
}

func (f *executionFlags) WorkerEnvs() []map[string]string {
	workerEnvs := []map[string]string{}
	for i := 0; i < *f.jobs; i++ {
		workerEnvs = append(workerEnvs,
			map[string]string{
				"QA_WORKER": fmt.Sprintf("%d", i),
				"TEST_ENV_NUMBER": fmt.Sprintf("%d", i),
			})
	}

	return workerEnvs
}

func (f *executionFlags) NewRunnerConfig(env *cmd.Env, runnerName string, patterns []string) runner.Config {
	return runner.Config{
		Name:         runnerName,
		FileLister:   runner.NewFileGlob(env.Dir, patterns),
		Seed:         *f.seed,
		Dir:          env.Dir,
		EnvVars:      env.Vars,
		SquashPolicy: *f.squashPolicy,
		// Enable entries below to add specific method calls (and optionally their arguments) to the trace.
		TraceProbes: []string{
		// "Kernel#require(path)",
		// "Kernel#load",
		// "ActiveRecord::ConnectionAdapters::Mysql2Adapter#execute(sql,name)",
		// "ActiveRecord::ConnectionAdapters::PostgresSQLAdapter#execute_and_clear(sql,name,binds)",
		// "ActiveSupport::Dependencies::Loadable#require(path)",
		// "ActiveRecord::ConnectionAdapters::QueryCache#clear_query_cache",
		// "ActiveRecord::ConnectionAdapters::SchemaCache#initialize",
		// "ActiveRecord::ConnectionAdapters::SchemaCache#clear!",
		// "ActiveRecord::ConnectionAdapters::SchemaCache#clear_table_cache!",
		},
		PassthroughConfig: map[string](interface{}){
			"eagerLoad":           *f.eagerLoad,
			"warmup":              *f.warmup,
			"errorsCaptureLocals": *f.errorsCaptureLocals,
			"captureStandardFds":  *f.captureStandardFds,
			"evalBeforeFork":      *f.evalBeforeFork,
			"evalAfterFork":       *f.evalAfterFork,
			"sampleStack":         *f.sampleStack,
		},
	}
}

func (f *executionFlags) ParseRunnerConfigs(env *cmd.Env, runnerSpecs []string) []runner.Config {
	var configs []runner.Config
	for _, runnerSpec := range runnerSpecs {
		runnerSpecSplit := strings.Split(runnerSpec, ":")
		runnerName := runnerSpecSplit[0]
		var patterns []string
		if len(runnerSpecSplit) == 1 {
			patterns = []string{run.DefaultGlob(runnerName)}
		} else {
			patterns = runnerSpecSplit[1:]
		}

		configs = append(configs, f.NewRunnerConfig(env, runnerName, patterns))
	}

	return configs
}
