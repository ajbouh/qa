package cmd

import (
	"io"
	"os/exec"
)

type Env struct {
	Vars   map[string]string
	Dir    string
	Stdin  io.Reader
	Stderr io.Writer
	Stdout io.Writer
}

func (env *Env) ApplyTo(cmd *exec.Cmd) {
	cmd.Dir = env.Dir
	cmd.Stdin = env.Stdin
	cmd.Stderr = env.Stderr
	cmd.Stdout = env.Stdout
}
