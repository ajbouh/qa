package top

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, argv []string) error {
	return analysis.RunRuby(env, "tapj-report.rb", argv[1:]...)
}
