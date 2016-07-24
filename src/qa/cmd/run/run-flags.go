package run

import (
	"encoding/json"
	"flag"
	"path/filepath"
	"qa/cmd"
	"strconv"
)

type runFlags struct {
	outputFlags    *outputFlags
	executionFlags *executionFlags

	chdir          *string
	suiteCoderef   *string
	suiteLabel     *string
}

func DefineFlags(flags *flag.FlagSet) *runFlags {
	return &runFlags{
		outputFlags:    defineOutputFlags(flags),
		executionFlags: defineExecutionFlags(flags),
		chdir:          flags.String("chdir", "", "Change to the given directory"),
		suiteCoderef:   flags.String("suite-coderef", "", "Set coderef for suite (useful for flakiness detection)"),
		suiteLabel:     flags.String("suite-label", "", "Set label for suite (useful for flakiness detection)"),
	}
}

func (f *runFlags) NewEnv(env *cmd.Env, runnerSpecs []string) (*Env, error) {
	executionFlags := *f.executionFlags
	outputFlags := *f.outputFlags
	e := *env

	if *f.chdir != "" {
		if filepath.IsAbs(*f.chdir) {
			e.Dir = *f.chdir
		} else {
			e.Dir = filepath.Join(e.Dir, *f.chdir)
		}
	}

	if *outputFlags.saveStacktraces != "" ||
		*outputFlags.saveFlamegraph != "" ||
		*outputFlags.saveIcegraph != "" {
		*executionFlags.sampleStack = true
	}

	svgTitleArgs, _ := json.Marshal(runnerSpecs)
	svgTitleSuffix := " — jobs = " + strconv.Itoa(*executionFlags.jobs) + ", runnerSpecs = " + string(svgTitleArgs)

	visitor, err := outputFlags.newVisitor(&e, *executionFlags.jobs, string(svgTitleSuffix))
	if err != nil {
		return nil, err
	}

	srv, err := executionFlags.Listen()
	if err != nil {
		return nil, err
	}

	return &Env{
		Seed:          *executionFlags.seed,
		SuiteLabel:    *f.suiteLabel,
		SuiteCoderef:  *f.suiteCoderef,
		WorkerEnvs:    executionFlags.WorkerEnvs(),
		RunnerConfigs: executionFlags.RunnerConfigs(&e, runnerSpecs),
		Visitor:       visitor,
		Server:        srv,
	}, nil
}
