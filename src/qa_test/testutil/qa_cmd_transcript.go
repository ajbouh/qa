package testutil

import (
	"bytes"
	"io"
	"os"
	"qa/cmd"
	"qa/tapjio"
)

type Transcript struct {
	Stderr            string
	Events            []interface{}
	SuiteBeginEvents       []tapjio.SuiteBeginEvent
	TestFinishEvents        []tapjio.TestFinishEvent
	TestBeginEvents []tapjio.TestBeginEvent
	TraceEvents       []tapjio.TraceEvent
	SuiteFinishEvents       []tapjio.SuiteFinishEvent
}

func NewTranscriptBuilder() (*Transcript, tapjio.Visitor) {
	tscript := &Transcript{}
	return tscript, &tapjio.DecodingCallbacks{
		OnSuiteBegin: func(event tapjio.SuiteBeginEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.SuiteBeginEvents = append(tscript.SuiteBeginEvents, event)
			return nil
		},
		OnTestBegin: func(event tapjio.TestBeginEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TestBeginEvents = append(tscript.TestBeginEvents, event)
			return nil
		},
		OnTestFinish: func(event tapjio.TestFinishEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TestFinishEvents = append(tscript.TestFinishEvents, event)
			return nil
		},
		OnTrace: func(event tapjio.TraceEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TraceEvents = append(tscript.TraceEvents, event)
			return nil
		},
		OnSuiteFinish: func(event tapjio.SuiteFinishEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.SuiteFinishEvents = append(tscript.SuiteFinishEvents, event)
			return nil
		},
	}
}

type QaCmd func(*cmd.Env, []string) error

func RunQaCmd(fn QaCmd, visitor tapjio.Visitor, stdin io.Reader, dir string, args []string) (stderr string, err error) {
	var stderrBuf bytes.Buffer

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()

	errCh := make(chan error, 2)
	go func() {
    env := &cmd.Env{Stdin: stdin, Stdout: wr, Stderr: os.Stderr, Dir: dir}
		errCh <- fn(env, args)

		wr.Close()
	}()

	err = tapjio.DecodeReader(rd, visitor)

	if err == nil {
		err = <-errCh
	}

	stderr = stderrBuf.String()
	return
}
