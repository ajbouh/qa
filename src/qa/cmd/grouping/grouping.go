package grouping

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, args []string) error {
  return analysis.RunRuby(env, "tapj-grouping.rb", args...)
}
