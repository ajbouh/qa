package qa_test

import (
	"qa/cmd"
	"qa/cmd/run"
	"qa/tapjio"
	"qa_test/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runQa(dir string) (*testutil.Transcript, error) {
	tscript, visitor := testutil.NewTranscriptBuilder()

	var err error
	tscript.Stderr, err = testutil.RunQaCmd(run.Main, visitor, nil, dir, []string{
		"-format=tapj",
		"-listen-network", "tcp",
		"-listen-address", "127.0.0.1:0",
		"rspec",
		"minitest:test/minitest/**/test*.rb",
		"test-unit:test/test-unit/**/test*.rb",
	})

	return tscript, err
}

func findTestFinishEvent(events []tapjio.TestFinishEvent, label string) tapjio.TestFinishEvent {
	for _, event := range events {
		if event.Label == label {
			return event
		}
	}

	return tapjio.TestFinishEvent{}
}

func TestRun(t *testing.T) {
	var tscript *testutil.Transcript
	var err error

	tscript, err = runQa("fixtures/ruby/simple")
	require.NoError(t, err, "qa failed: %s", tscript.Stderr)

	testEventLabelsExpectingStandardFds := []string{
		"test_library_minitest",
		"test_library_test_unit",
		"my library rspec",
	}
	for _, label := range testEventLabelsExpectingStandardFds {
		testEvent := findTestFinishEvent(tscript.TestFinishEvents, label)
		require.Contains(t, testEvent.Stdout, "Created MyLibrary [out]")
		require.Contains(t, testEvent.Stderr, "Created MyLibrary [err]")
	}

	require.Equal(t,
		tapjio.ResultTally{Total: 6, Pass: 6},
		*tscript.SuiteFinishEvents[0].Counts,
		"wrong count in final event. Events: %#v, Stderr: %v\n", tscript.Events, tscript.Stderr)

	tscript, err = runQa("fixtures/ruby/all-outcomes")
	require.Error(t, err, "qa should have failed: %s", tscript.Stderr)

	require.Equal(t,
		tapjio.ResultTally{Total: 20, Pass: 4, Fail: 4, Todo: 4, Error: 8},
		*tscript.SuiteFinishEvents[0].Counts,
		"wrong count in final event. Events: %#v, Stderr: %v\n", tscript.Events, tscript.Stderr)
}

func runQaFramework(frameworkName, dir, glob string) (*testutil.Transcript, error) {
	tscript, visitor := testutil.NewTranscriptBuilder()

	var err error
	fn := func(env *cmd.Env, args []string) error {
		return run.Framework(frameworkName, env, args)
	}

	tscript.Stderr, err = testutil.RunQaCmd(fn, visitor, nil, dir, []string{
		"-format=tapj",
		"-listen-network", "tcp",
		"-listen-address", "127.0.0.1:0",
		glob,
	})

	return tscript, err
}

type qaFrameworkTest struct {
	frameworkName string
	dir           string
	glob          string
	tally         tapjio.ResultTally
}

func testFramework(t *testing.T, ix int, test qaFrameworkTest) {
	var tscript *testutil.Transcript
	var err error

	startingTime := time.Now()
	tscript, err = runQaFramework(test.frameworkName, test.dir, test.glob)
	duration := time.Now().Sub(startingTime)

	if test.tally.Total == test.tally.Pass {
		require.NoError(t, err, "%v. qa failed: %s", ix, tscript.Stderr)
	} else {
		require.Error(t, err, "%v. qa should have failed.", ix, tscript.Stderr)
	}

	// Expect suite event to have time ≥ when we started qa
	suiteEvent := tscript.SuiteBeginEvents[0]
	suiteStartTime, err := time.Parse("2006-01-02 15:04:05", suiteEvent.Start)
	require.NoError(t, err, "%v. Invalid suite start time: %s", ix, suiteEvent.Start)

	require.InDelta(t,
			startingTime.UnixNano(),
			suiteStartTime.UnixNano(),
			(duration.Seconds() + 1) * 1e9, // Add 1 second to avoid issues with time resolution
			"%v. Suite time (%v) too far from current time (%v)", ix, suiteStartTime, startingTime)

	// Expect enough test events, all should have a time ≥ when we started qa
	require.Equal(t, test.tally.Total, len(tscript.TestBeginEvents),
			"%v. Wrong number of test begin events.", ix)
	for _, testEvent := range tscript.TestBeginEvents {
		require.Equal(t, true, testEvent.Timestamp >= float64(startingTime.Unix()),
				"%v. Test timestamp (%v) should be on or after initial time (%v).", ix, testEvent.Timestamp, startingTime)
	}

	require.Equal(t, test.tally.Total, len(tscript.TestFinishEvents), "Wrong number of test events.")
	for _, testEvent := range tscript.TestFinishEvents {
		require.Equal(t, true, testEvent.Time <= duration.Seconds(),
				"%v. Test duration (%v) should be less than or equal to the total duration (%v).", ix, testEvent.Time, duration)
	}

	require.Equal(t,
		test.tally,
		*tscript.SuiteFinishEvents[0].Counts,
		"%v. Wrong count in final event. Events: %#v, Stderr: %v\n", ix, tscript.Events, tscript.Stderr)
}

var qaFrameworkTests = []qaFrameworkTest{
	{"rspec", "fixtures/ruby/simple", "spec/**/*spec.rb", tapjio.ResultTally{Total: 2, Pass: 2}},
	{"rspec", "fixtures/ruby/all-outcomes", "spec/**/*spec.rb", tapjio.ResultTally{Total: 6, Pass: 1, Fail: 1, Todo: 1, Error: 3}},
	{"minitest", "fixtures/ruby/simple", "test/minitest/**/test*.rb", tapjio.ResultTally{Total: 2, Pass: 2}},
	{"minitest", "fixtures/ruby/all-outcomes", "test/minitest/**/test*.rb", tapjio.ResultTally{Total: 5, Pass: 1, Fail: 1, Todo: 1, Error: 2}},
	{"test-unit", "fixtures/ruby/simple", "test/test-unit/**/test*.rb", tapjio.ResultTally{Total: 2, Pass: 2}},
	{"test-unit", "fixtures/ruby/all-outcomes", "test/test-unit/**/test*.rb", tapjio.ResultTally{Total: 9, Pass: 2, Fail: 2, Todo: 2, Error: 3}},
}

func TestFrameworks(t *testing.T) {
	for ix, framework := range qaFrameworkTests {
		testFramework(t, ix, framework)
	}
}
