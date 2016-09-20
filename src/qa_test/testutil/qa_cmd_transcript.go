package testutil

import (
	"bytes"
	"io"
	"qa/cmd"
	"qa/tapjio"
	"sync"
)

type Transcript struct {
	Stderr            string
	Events            []interface{}
	SuiteBeginEvents  []tapjio.SuiteBeginEvent
	TestFinishEvents  []tapjio.TestFinishEvent
	TestBeginEvents   []tapjio.TestBeginEvent
	TraceEvents       []tapjio.TraceEvent
	SuiteFinishEvents []tapjio.SuiteFinishEvent
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

type buffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *buffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}
func (b *buffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}

func RunQaCmd(fn QaCmd, visitor tapjio.Visitor, stdin io.Reader, dir string, args []string) (stderr string, err error) {
	var stderrBuf = buffer{bytes.Buffer{}, sync.Mutex{}}

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()

	errCh := make(chan error, 2)
	go func() {
		env := &cmd.Env{Stdin: stdin, Stdout: wr, Stderr: &stderrBuf, Dir: dir}
		errCh <- fn(env, append([]string{"cmd"}, args...))

		wr.Close()
	}()

	err = tapjio.DecodeReader(rd, visitor)

	if err == nil {
		err = <-errCh
	}

	stderr = stderrBuf.String()
	return
}
