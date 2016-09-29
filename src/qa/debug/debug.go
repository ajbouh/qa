package debug

import (
	"bytes"
	"fmt"
	"os/exec"
	"qa/cmd"
	"strconv"
)

func Abort(env *cmd.Env, kind string, host string, port int) error {
	cmd := exec.Command("pry-remote",
		"--server", host,
		"--port", strconv.Itoa(port))
	cmd.Dir = env.Dir
	cmd.Stdin = bytes.NewBuffer([]byte{})
	cmd.Stdout = nil
	cmd.Stderr = nil

	cmd.Start()
	err := cmd.Wait()

	return err
}

func Attach(env *cmd.Env, kind string, host string, port int) error {
	cmd := exec.Command("pry-remote",
		"--server", host,
		"--port", strconv.Itoa(port))
	cmd.Dir = env.Dir
	cmd.Stdin = env.Stdin
	cmd.Stdout = env.Stdout
	cmd.Stderr = env.Stderr

	cmd.Start()
	err := cmd.Wait()

	fmt.Fprintf(cmd.Stderr, "\n")

	return err
}
