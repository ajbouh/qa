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
	"qa/tapjio"
)

type executionFlags struct {
	jobs                *int
	runs                *int
	squashPolicy        *runner.SquashPolicy
	listenNetwork       *string
	listenAddress       *string
	debugErrorClass     *string
	debugErrorsWith     *string
	debugListenHost     *string
	debugOnlyOutcome    *string
	doneAfterDebug      *bool
	errorsCaptureLocals *bool
	captureStandardFds  *bool
	evalBeforeFork      *string
	evalAfterFork       *string
	sampleStack         *bool
	filter              *string
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

func defineExecutionFlags(vars map[string]string, flags *flag.FlagSet) *executionFlags {
	squashPolicyValue := &squashPolicyValue{new(runner.SquashPolicy)}
	*squashPolicyValue.value = runner.SquashByFile
	flags.Var(squashPolicyValue, "squash", "One of: all, none, file")

	return &executionFlags{
		seed:                flags.Int("seed", -1, "Set seed to use"),
		jobs:                flags.Int("jobs", runtime.NumCPU(), "Set number of jobs"),
		runs:                flags.Int("runs", 1, "Set number of times to run tests"),
		squashPolicy:        squashPolicyValue.value,
		listenNetwork:       flags.String("listen-network", "tcp", "Specify unix or tcp socket for worker coordination"),
		listenAddress:       flags.String("listen-address", "127.0.0.1:0", "Listen address for worker coordination"),
		debugErrorClass:     flags.String("debug-error-class", "", "Specify which, if any, error class to debug"),
		debugErrorsWith:     flags.String("debug-errors-with", "", "Specify which, if any, debug engine to use. Options: pry-remote"),
		debugListenHost:     flags.String("debug-listen-host", "127.0.0.1", "Specify which address to use when waiting for debug client to attach"),
		debugOnlyOutcome:    flags.String("debug-only-outcome", "", "Specify an outcome digest to limit debugging to"),
		doneAfterDebug:      flags.Bool("done-after-debug", true, "Whether or not to continue running tests after debug session"),
		errorsCaptureLocals: flags.Bool("errors-capture-locals", true, "Use runtime debug API to capture local variables when raising errors"),
		captureStandardFds:  flags.Bool("capture-standard-fds", true, "Capture stdout and stderr"),
		evalBeforeFork:      flags.String("eval-before-fork", "", "Execute the given code before forking any workers or loading any files"),
		evalAfterFork:       flags.String("eval-after-fork", "", "Execute the given code after a worker forks, but before work begins"),
		sampleStack:         flags.Bool("sample-stack", false, "Enable stack sampling"),
		warmup:              flags.Bool("warmup", true, "Use a variety of experimental heuristics to warm up worker caches"),
		filter:              flags.String("filter", "", "Specify a single test filter to run"),
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
				"QA_WORKER":       fmt.Sprintf("%d", i),
				"TEST_ENV_NUMBER": fmt.Sprintf("%d", i),
			})
	}

	return workerEnvs
}

func (f *executionFlags) NewRunnerConfig(env *cmd.Env, runnerName string, patterns []string) runner.Config {
	var filters []tapjio.TestFilter
	if *f.filter != "" {
		filters = append(filters, tapjio.TestFilter(*f.filter))
	}
	return runner.Config{
		Name:         runnerName,
		FileLister:   runner.NewFileGlob(env.Dir, patterns),
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
		Filters: filters,
		PassthroughConfig: map[string](interface{}){
			"eagerLoad":           *f.eagerLoad,
			"warmup":              *f.warmup,
			"debugErrorClass":     *f.debugErrorClass,
			"debugErrorsWith":     *f.debugErrorsWith,
			"debugListenHost":     *f.debugListenHost,
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
