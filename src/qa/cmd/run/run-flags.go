package run

import (
	"crypto/rand"
	"flag"
	"fmt"
	"math/big"
	"path/filepath"
	"qa/cmd"
	"qa/debug"
	"qa/run"
	"qa/runner"
	"qa/tapjio"
)

type runFlags struct {
	outputFlags    *outputFlags
	executionFlags *executionFlags

	chdir        *string
	suiteCoderef *string
	suiteLabel   *string
	watch        *bool

	memprofile *string
	heapdump   *string
}

func DefineFlags(vars map[string]string, flags *flag.FlagSet) *runFlags {
	return &runFlags{
		outputFlags:    defineOutputFlags(vars, flags),
		executionFlags: defineExecutionFlags(vars, flags),
		chdir:          flags.String("chdir", "", "Change to the given directory"),
		suiteCoderef:   flags.String("suite-coderef", "", "Set coderef for suite (useful for flakiness detection)"),
		suiteLabel:     flags.String("suite-label", "", "Set label for suite (useful for flakiness detection)"),
		watch:          flags.Bool("watch", false, "Watch test files for changes and continuously re-run tests"),
		memprofile:     flags.String("memprofile", "", "write memory profile to `file`"),
		heapdump:       flags.String("heapdump", "", "write heap dump to `file`"),
	}
}

func (f *runFlags) cloneAndAdjustEnv(env *cmd.Env) *cmd.Env {
	e := new(cmd.Env)
	*e = *env

	if *f.chdir != "" {
		if filepath.IsAbs(*f.chdir) {
			e.Dir = *f.chdir
		} else {
			e.Dir = filepath.Join(e.Dir, *f.chdir)
		}
	}

	return e
}

func (f *runFlags) Watch() bool {
	return *f.watch
}

func (f *runFlags) SetShowSnails(showSnails bool) {
	*f.outputFlags.showSnails = showSnails
}

func (f *runFlags) SetShowUpdatingSummary(showUpdatingSummary bool) {
	*f.outputFlags.showUpdatingSummary = showUpdatingSummary
}

func (f *runFlags) ApplyImpliedDefaults() {
	executionFlags := f.executionFlags
	outputFlags := f.outputFlags

	if *outputFlags.saveStacktraces != "" ||
		*outputFlags.saveFlamegraph != "" ||
		*outputFlags.saveIcegraph != "" {
		*executionFlags.sampleStack = true
	}

	if *f.watch {
		f.SetShowSnails(false)
		f.SetShowUpdatingSummary(false)
	}
}

func (f *runFlags) NewRunnerConfig(env *cmd.Env, runnerName string, patterns []string) runner.Config {
	return f.executionFlags.NewRunnerConfig(f.cloneAndAdjustEnv(env), runnerName, patterns)
}

func (f *runFlags) ParseRunnerConfigs(env *cmd.Env, runnerSpecs []string) []runner.Config {
	return f.executionFlags.ParseRunnerConfigs(f.cloneAndAdjustEnv(env), runnerSpecs)
}

func (f *runFlags) NewEnv(env *cmd.Env, runnerConfigs []runner.Config) (*run.Env, error) {
	executionFlags := *f.executionFlags
	outputFlags := *f.outputFlags

	runs := *executionFlags.runs
	varyingSeeds := *executionFlags.seed == -1

	svgTitleSuffix := fmt.Sprintf(" — jobs = %d, runs = %d, runnerSpecs = %#v",
		*executionFlags.jobs, runs, runnerConfigs)

	e := f.cloneAndAdjustEnv(env)
	visitor, err := outputFlags.newVisitor(e, *executionFlags.jobs, runs, varyingSeeds, svgTitleSuffix)
	if err != nil {
		return nil, err
	}

	doneAfterDebug := *executionFlags.doneAfterDebug
	debugOnlyOutcome := tapjio.OutcomeDigest(*executionFlags.debugOnlyOutcome)
	if *executionFlags.debugErrorsWith != "" {
		var mostRecentOutcome tapjio.OutcomeDigest
		visitor = tapjio.MultiVisitor(
			[]tapjio.Visitor{
				visitor,
				&tapjio.DecodingCallbacks{
					OnTestBegin: func(event tapjio.TestBeginEvent) error {
						mostRecentOutcome = tapjio.NoOutcome
						return nil
					},
					OnTestFinish: func(event tapjio.TestFinishEvent) error {
						var err error
						mostRecentOutcome, err = tapjio.OutcomeDigestFor(event.Status, event.Exception)
						return err
					},
					OnAwaitAttach: func(event tapjio.AwaitAttachEvent) error {
						if debugOnlyOutcome != tapjio.NoOutcome && debugOnlyOutcome != mostRecentOutcome {
							return debug.Abort(env, event.AwaitType, event.Host, event.Port)
						}

						err := debug.Attach(env, event.AwaitType, event.Host, event.Port)
						if err != nil {
							return err
						}

						if doneAfterDebug {
							return &cmd.QuietError{0}
						}

						return nil
					},
				},
			},
		)
	}

	srv, err := executionFlags.Listen()
	if err != nil {
		return nil, err
	}

	return &run.Env{
		SeedFn: func(repetition int) int {
			if !varyingSeeds {
				return *executionFlags.seed
			}

			bigSeed, err := rand.Int(rand.Reader, big.NewInt(65535))
			if err != nil {
				panic(err)
			}
			return int(bigSeed.Int64())
		},
		SuiteLabel:    *f.suiteLabel,
		SuiteCoderef:  *f.suiteCoderef,
		Runs:          *executionFlags.runs,
		Memprofile:    *f.memprofile,
		Heapdump:      *f.heapdump,
		WorkerEnvs:    executionFlags.WorkerEnvs(),
		RunnerConfigs: runnerConfigs,
		Visitor:       visitor,
		Server:        srv,
	}, nil
}
