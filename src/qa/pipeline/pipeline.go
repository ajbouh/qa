package pipeline

import (
	"fmt"
	"io"
	"qa/cmd"
	"sync"
)

type Op struct {
	Main func(*cmd.Env, []string) error
	Argv []string
}

func Run(env *cmd.Env, ops []Op) error {
	errChan := make(chan error, len(ops))

	stderrRd, stderrWr := io.Pipe()
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	defer stderrRd.Close()
	defer stderrWr.Close()
	go func() {
		defer stderrWg.Done()
		io.Copy(env.Stderr, stderrRd)
	}()

	stdin := env.Stdin
	for ix, op := range ops {

		var opEnv *cmd.Env
		var closer io.Closer
		if ix == len(ops)-1 {
			opEnv = &cmd.Env{Stdin: stdin, Stdout: env.Stdout, Stderr: stderrWr}
		} else {
			rd, wr := io.Pipe()
			defer rd.Close()
			defer wr.Close()
			opEnv = &cmd.Env{Stdin: stdin, Stdout: wr, Stderr: stderrWr}
			stdin = rd
			closer = wr
		}

		go func(opEnv *cmd.Env, op Op, closer io.Closer) {
			if closer != nil {
				defer closer.Close()
			}
			errChan <- op.Main(opEnv, op.Argv)
		}(opEnv, op, closer)
	}

	errs := []error{}
	receiveCount := 0
	for err := range errChan {
		receiveCount++

		if err != nil {
			errs = append(errs, err)
		}

		if receiveCount == len(ops) {
			close(errChan)
		}
	}

	stderrWr.Close()

	stderrWg.Wait()

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf("Pipeline failed: %v", errs)
}
