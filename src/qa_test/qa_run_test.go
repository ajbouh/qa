package qa_test

import (
	"qa/cmd/run"
	"qa/tapjio"
	"qa_test/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TODO which ruby version must qa want to run?
func runQa(dir string) (*testutil.Transcript, error) {
	tscript, visitor := testutil.NewTranscriptBuilder()

	var err error
	tscript.Stderr, err = testutil.RunQaCmd(run.Main, visitor, dir, []string{
		"-format=tapj",
		"-listen-network", "tcp",
		"-listen-address", "127.0.0.1:0",
		"rspec",
		"minitest:test/minitest/**/test*.rb",
		"test-unit:test/test-unit/**/test*.rb",
	})

	return tscript, err
}

func findTestEvent(events []tapjio.TestEvent, label string) tapjio.TestEvent {
	for _, event := range events {
		if event.Label == label {
			return event
		}
	}

	return tapjio.TestEvent{}
}

func TestRun(t *testing.T) {
	var tscript *testutil.Transcript
	var err error

	startingTime := time.Now()
	tscript, err = runQa("fixtures/ruby/simple")
	finalTime := time.Now()
	require.NoError(t, err, "qa failed: %s", tscript.Stderr)

	// Expect suite event to have time ≥ when we started qa
	suiteEvent := tscript.SuiteEvents[0]
	suiteStartTime, err := time.Parse("2006-01-02 15:04:05", suiteEvent.Start)
	require.NoError(t, err, "Invalid suite start time: %s", suiteEvent.Start)

	duration := finalTime.Sub(startingTime)
	require.InDelta(t, startingTime.UnixNano(), suiteStartTime.UnixNano(), duration.Seconds() * 1e9,
			"Suite time (%v) too far from current time (%v)", suiteStartTime, startingTime)

	// Expect 6 test events, all should have a time ≥ when we started qa
	require.Equal(t, 6, len(tscript.TestStartedEvents), "Wrong number of test begin events.")
	for _, testEvent := range tscript.TestStartedEvents {
		require.Equal(t, true, testEvent.Timestamp >= float64(startingTime.Unix()),
				"Test timestamp (%v) should be on or after initial time (%v).", testEvent.Timestamp, startingTime)
	}

	require.Equal(t, 6, len(tscript.TestEvents), "Wrong number of test events.")
	for _, testEvent := range tscript.TestEvents {
		require.Equal(t, true, testEvent.Time <= duration.Seconds(),
				"Test duration (%v) should be less than or equal to the total duration (%v).", testEvent.Time, duration)
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

	require.Equal(t,
		tapjio.ResultTally{Total: 6, Pass: 6},
		*tscript.FinalEvents[0].Counts,
		"wrong count in final event. Events: %#v, Stderr: %v\n", tscript.Events, tscript.Stderr)

	tscript, err = runQa("fixtures/ruby/all-outcomes")
	require.Error(t, err, "qa should have failed: %s", tscript.Stderr)

	require.Equal(t,
		tapjio.ResultTally{Total: 20, Pass: 4, Fail: 4, Todo: 4, Error: 8},
		*tscript.FinalEvents[0].Counts,
		"wrong count in final event. Events: %#v, Stderr: %v\n", tscript.Events, tscript.Stderr)
}
