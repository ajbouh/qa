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

	chdir *string
}

func DefineFlags(flags *flag.FlagSet) *runFlags {
	return &runFlags{
		outputFlags:    defineOutputFlags(flags),
		executionFlags: defineExecutionFlags(flags),
		chdir:          flags.String("chdir", "", "Change to the given directory"),
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
		WorkerEnvs:    executionFlags.WorkerEnvs(),
		RunnerConfigs: executionFlags.RunnerConfigs(&e, runnerSpecs),
		Visitor:       visitor,
		Server:        srv,
	}, nil
}
