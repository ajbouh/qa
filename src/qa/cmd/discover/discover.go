package discover

import (
	"qa/analysis"
	"qa/cmd"
)

func Main(env *cmd.Env, argv []string) error {
	return analysis.RunRuby(env, "tapj-discover.rb", argv[1:]...)
}
