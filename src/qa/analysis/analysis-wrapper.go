package analysis

import (
	"os/exec"
	"qa/analysis/assets"
	"qa/cmd"
)

//go:generate go-bindata -o $GOGENPATH/qa/analysis/assets/bindata.go -pkg assets -prefix ../analysis-assets/ ../analysis-assets/...

func RunRuby(env *cmd.Env, runType string, args ...string) error {
	data, err := assets.Asset(runType)
	if err != nil {
		return err
	}
	rubyArgs := append(
		[]string{
			"-e",
			string(data),
			"--",
		},
		args...)
	cmd := exec.Command("ruby", rubyArgs...)

	env.ApplyTo(cmd)

	return cmd.Run()
}
