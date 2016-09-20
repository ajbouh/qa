package grouping

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, argv []string) error {
	return analysis.RunRuby(env, "tapj-grouping.rb", argv[1:]...)
}
