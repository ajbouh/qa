package discover

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, args []string) error {
  return analysis.RunRuby(env, "tapj-discover.rb", args...)
}
