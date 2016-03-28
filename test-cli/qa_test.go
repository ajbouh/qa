package qa_test

// go test .

import (
	"bytes"
	"os/exec"
	"path"
	"qa/tapj"
	"testing"
)

// TODO which ruby version must qa want to run?
func runQa(t *testing.T, dir string) (events []interface{}, stderr string, err error) {
	events = make([]interface{}, 0)

	cmd := exec.Command("qa", "-format=tapj")
	cmd.Dir = dir
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, stdoutErr := cmd.StdoutPipe()
	if stdoutErr != nil {
		err = stdoutErr
		return
	}

	if err = cmd.Start(); err != nil {
		return
	}

	err = tapj.Decoder{stdout}.Decode(
		&tapj.DecodingCallbacks{
			OnEvent: func(event interface{}) error { events = append(events, event); return nil },
		})

	cmdErr := cmd.Wait()
	if err == nil {
		err = cmdErr
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
		t.Fatal("qa failed here.", stderr)
	}

	if len(events) == 0 {
		t.Fatal("No events for tests in", baseDir, stderr)
	}

	finalEvent := events[len(events)-1]
	if fe, ok := finalEvent.(*tapj.FinalEvent); ok {
		expect := tapj.FinalEvent{
			Type:   fe.Type,
			Time:   fe.Time,
			Counts: tapj.FinalCounts{Total: 6, Pass: 6},
		}

		if expect != *fe {
			t.Fatal("wrong count in final event.", expect, "vs", *fe, events, stderr)
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
	if fe, ok := finalEvent.(*tapj.FinalEvent); ok {
		expect := tapj.FinalEvent{
			Type: fe.Type,
			Time: fe.Time,
			Counts: tapj.FinalCounts{
				Total: 12,
				Pass:  3,
				Fail:  3,
				Todo:  3,
				Error: 3,
			},
		}

		if expect != *fe {
			t.Fatal("wrong count in final event.", expect, "vs", fe, events, stderr)
		}
	} else {
		t.Fatal("last event wasn't a final event.", events, stderr)
	}
}

func TestRuby(t *testing.T) {
	testRunner(t, "fixtures/ruby")
}
