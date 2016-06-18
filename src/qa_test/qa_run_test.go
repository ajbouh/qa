package qa_test

import (
	"bytes"
	"io"
	"path"
	"qa/cmd"
	"qa/cmd/run"
	"qa/tapjio"
	"testing"

	"github.com/stretchr/testify/require"
)

type transcript struct {
	Stderr            string
	Events            []interface{}
	SuiteEvents       []tapjio.SuiteEvent
	TestEvents        []tapjio.TestEvent
	TestStartedEvents []tapjio.TestStartedEvent
	TraceEvents       []tapjio.TraceEvent
	FinalEvents       []tapjio.FinalEvent
}

// TODO which ruby version must qa want to run?
func runQa(t *testing.T, dir string) (tscript transcript, err error) {
	tscript.Events = make([]interface{}, 0)

	var stderrBuf bytes.Buffer

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run.Main(
			&cmd.Env{Stdout: wr, Stderr: &stderrBuf, Dir: dir},
			[]string{
				"-format=tapj",
				"rspec",
				"minitest:test/minitest/**/test*.rb",
				"test-unit:test/test-unit/**/test*.rb",
			})

		wr.Close()
	}()

	err = tapjio.Decode(rd,
		&tapjio.DecodingCallbacks{
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
		})

	if err == nil {
		err = <-errCh
	}

	tscript.Stderr = stderrBuf.String()
	return
}

func findTestEvent(events []tapjio.TestEvent, label string) tapjio.TestEvent {
	for _, event := range events {
		if event.Label == label {
			return event
		}
	}

	return tapjio.TestEvent{}
}

func TestRuby(t *testing.T) {
	baseDir := "fixtures/ruby"
	var err error
	var tscript transcript

	tscript, err = runQa(t, path.Join(baseDir, "simple"))
	if err != nil {
		t.Fatal("qa failed here.", err, tscript.Stderr)
	}

	if len(tscript.Events) == 0 {
		t.Fatal("No events for tests in", baseDir, tscript.Stderr)
	}

	testEventLabelsExpectingStandardFds := []string{
		"test_library_minitest",
		"test_library_test_unit",
		"my library rspec",
	}
	for _, label := range testEventLabelsExpectingStandardFds {
		testEvent := findTestEvent(tscript.TestEvents, label)
		require.Contains(t, testEvent.Stdout, "Created MyLibrary [out]")
		require.Contains(t, testEvent.Stderr, "Created MyLibrary [err]")
	}

	finalEvent := tscript.Events[len(tscript.Events)-1]
	if fe, ok := finalEvent.(tapjio.FinalEvent); ok {
		expect := tapjio.ResultTally{Total: 6, Pass: 6}

		if expect != *fe.Counts {
			t.Fatal("wrong count in final event.", expect, "vs", *fe.Counts, tscript.Events, tscript.Stderr)
		}
	} else {
		t.Fatal("last event wasn't a final event.", tscript.Events, tscript.Stderr)
	}

	tscript, err = runQa(t, path.Join(baseDir, "all-outcomes"))
	if err == nil {
		t.Fatal("qa should have failed.", tscript.Stderr)
	}

	if len(tscript.Events) == 0 {
		t.Fatal("No events for tests in", baseDir, tscript.Stderr)
	}

	finalEvent = tscript.Events[len(tscript.Events)-1]
	if fe, ok := finalEvent.(tapjio.FinalEvent); ok {
		expect := tapjio.ResultTally{
			Total: 16,
			Pass:  4,
			Fail:  4,
			Todo:  4,
			Error: 4,
		}

		if expect != *fe.Counts {
			t.Fatal("wrong count in final event.", expect, "vs", *fe.Counts, tscript.Events, tscript.Stderr)
		}
	} else {
		t.Fatal("last event wasn't a final event.", tscript.Events, tscript.Stderr)
	}
}
