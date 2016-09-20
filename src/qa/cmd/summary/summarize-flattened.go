package summary

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, argv []string) error {
	return analysis.RunRuby(env, "tapj-summary.rb", argv[1:]...)
}
