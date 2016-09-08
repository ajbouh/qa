package cmd

import (
	"fmt"
	"io"
	"os/exec"
)

type QuietError struct {
	Status int
}

func (e QuietError) Error() string {
	return fmt.Sprintf("exit code: %d", e.Status)
}

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
