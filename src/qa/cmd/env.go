package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

var copiedVars = []string {
	"QA_ARCHIVE",
}

func OsEnv() *Env {
	vars := make(map[string]string)
	for _, envLine := range os.Environ() {
		for _, copiedVar := range copiedVars {
			if strings.HasPrefix(envLine, copiedVar) {
				if envLine[len(copiedVar)] == '=' {
					vars[copiedVar] = os.Getenv(copiedVar)
				}
			}
		}
	}

	if len(vars) == 0 {
		vars = nil
	}

	return &Env{
		Stdin: os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Vars: vars,
	}
}
