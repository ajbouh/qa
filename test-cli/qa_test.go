package qa_test

// go test .

import (
	"bytes"
	"io"
	"path"
	"os"
	"qa/cmd/run"
	"qa/tapjio"
	"testing"
)

// TODO which ruby version must qa want to run?
func runQa(t *testing.T, dir string) (events []interface{}, stderr string, err error) {
	events = make([]interface{}, 0)

	var stderrBuf bytes.Buffer

	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run.Main(
			wr,
			os.Stderr,
			dir,
			[]string{
				"-format=tapj",
				"rspec:spec/**/*_spec.rb",
				"minitest:test/minitest/**/test*.rb",
				"test-unit:test/test-unit/**/test*.rb",
			})

		wr.Close()
	}()

	err = tapjio.Decode(rd,
		&tapjio.DecodingCallbacks{
			OnSuite: func(event tapjio.SuiteEvent) error {
				events = append(events, event)
				return nil
			},
			OnTestBegin: func(event tapjio.TestStartedEvent) error {
				events = append(events, event)
				return nil
			},
			OnTest:  func(event tapjio.TestEvent) error {
				events = append(events, event)
				return nil
			},
			OnTrace: func(event tapjio.TraceEvent) error {
				events = append(events, event)
				return nil
			},
			OnFinal: func(event tapjio.FinalEvent) error {
				events = append(events, event)
				return nil
			},
		})

	if err == nil {
		err = <-errCh
	}

	stderr = stderrBuf.String()
	return
}

func testRunner(t *testing.T, baseDir string) {
	var events []interface{}
	var err error
	var stderr string

	events, stderr, err = runQa(t, path.Join(baseDir, "simple"))
	if err != nil {
		t.Fatal("qa failed here.", err, stderr)
	}

	if len(events) == 0 {
		t.Fatal("No events for tests in", baseDir, stderr)
	}

	finalEvent := events[len(events)-1]
	if fe, ok := finalEvent.(tapjio.FinalEvent); ok {
		expect := tapjio.ResultTally{Total: 6, Pass: 6}

		if expect != *fe.Counts {
			t.Fatal("wrong count in final event.", expect, "vs", *fe.Counts, events, stderr)
		}
	} else {
		t.Fatal("last event wasn't a final event.", events, stderr)
	}

	events, stderr, err = runQa(t, path.Join(baseDir, "all-outcomes"))
	if err == nil {
		t.Fatal("qa should have failed.", stderr)
	}

	if len(events) == 0 {
		t.Fatal("No events for tests in", baseDir, stderr)
	}

	finalEvent = events[len(events)-1]
	if fe, ok := finalEvent.(tapjio.FinalEvent); ok {
		expect := tapjio.ResultTally{
			Total: 16,
			Pass:  4,
			Fail:  4,
			Todo:  4,
			Error: 4,
		}

		if expect != *fe.Counts {
			t.Fatal("wrong count in final event.", expect, "vs", *fe.Counts, events, stderr)
		}
	} else {
		t.Fatal("last event wasn't a final event.", events, stderr)
	}
}

func TestRuby(t *testing.T) {
	testRunner(t, "fixtures/ruby")
}
