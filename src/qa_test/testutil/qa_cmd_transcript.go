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
	SuiteEvents       []tapjio.SuiteEvent
	TestEvents        []tapjio.TestEvent
	TestStartedEvents []tapjio.TestStartedEvent
	TraceEvents       []tapjio.TraceEvent
	FinalEvents       []tapjio.FinalEvent
}

func NewTranscriptBuilder() (*Transcript, tapjio.Visitor) {
	tscript := &Transcript{}
	return tscript, &tapjio.DecodingCallbacks{
		OnSuite: func(event tapjio.SuiteEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.SuiteEvents = append(tscript.SuiteEvents, event)
			return nil
		},
		OnTestBegin: func(event tapjio.TestStartedEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TestStartedEvents = append(tscript.TestStartedEvents, event)
			return nil
		},
		OnTest: func(event tapjio.TestEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TestEvents = append(tscript.TestEvents, event)
			return nil
		},
		OnTrace: func(event tapjio.TraceEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.TraceEvents = append(tscript.TraceEvents, event)
			return nil
		},
		OnFinal: func(event tapjio.FinalEvent) error {
			tscript.Events = append(tscript.Events, event)
			tscript.FinalEvents = append(tscript.FinalEvents, event)
			return nil
		},
	}
}

type QaCmd func(*cmd.Env, []string) error

func RunQaCmd(fn QaCmd, visitor tapjio.Visitor, dir string, args []string) (stderr string, err error) {
	var stderrBuf bytes.Buffer

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()

	errCh := make(chan error, 2)
	go func() {
    env := &cmd.Env{Stdout: wr, Stderr: os.Stderr, Stdin: bytes.NewBuffer([]byte{}), Dir: dir}
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
