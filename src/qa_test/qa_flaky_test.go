package qa_test

import (
	"bytes"
	"encoding/json"
	"fmt"

	"io/ioutil"
	"os"
	"qa/cmd"
	"qa/cmd/flaky"
	"qa/cmd/run"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qaFlaky(args ...string) (interface{}, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	env := &cmd.Env{Stdout: &stdout, Stderr: &stderr}
	err := flaky.Main(env, args)
	if err != nil {
		return nil, fmt.Errorf("error running with %v (%v): %v", args, err, stderr.String())
	}

	summary := []map[string](interface{}){}
	if err = json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		return summary, err
	}

	return summary, nil
}

func TestDetectFlaky(t *testing.T) {
	dir, err := ioutil.TempDir("", "qa-archive")
	if err != nil {
		t.Fatal("Couldn't make temporary directory for qa archive")
	}
	defer os.RemoveAll(dir)

	qaRunArgs := []string{
		"-suite-label", "my-flaky-suite",
		"-suite-coderef", "r1",
		"-archive-base-dir", dir,
		"-listen-network", "tcp",
		"-listen-address", "127.0.0.1:0",
		"rspec",
		"minitest:test/minitest/**/test*.rb",
		"test-unit:test/test-unit/**/test*.rb",
	}
	var stderrBuf bytes.Buffer
	var stdoutBuf bytes.Buffer

	env := &cmd.Env{
		Stderr: &stderrBuf,
		Stdout: &stdoutBuf,
		Dir:    "fixtures/ruby/flaky",
		Vars:   map[string]string{},
	}

	env.Vars["QA_FLAKY_1"] = "true"
	env.Vars["QA_FLAKY_2"] = "false"
	env.Vars["QA_FLAKY_TYPE"] = "error"
	// Expect two flaky (error) fails for each test type.
	run.Main(env, qaRunArgs)
	run.Main(env, qaRunArgs)
	env.Vars["QA_FLAKY_TYPE"] = "assert"
	// Expect one flaky (assertion) fail for each test type
	run.Main(env, qaRunArgs)
	env.Vars["QA_FLAKY_1"] = "false"
	env.Vars["QA_FLAKY_2"] = "false"
	// Expect this entire run to pass.
	run.Main(env, qaRunArgs)
	env.Vars["QA_FLAKY_1"] = "false"
	env.Vars["QA_FLAKY_2"] = "true"
	// Expect a different kind of flaky (assertion) fail for each test type
	run.Main(env, qaRunArgs)
	qaRunArgs[3] = "r2"
	// Expect a failure similar to above, but still counted with r1.
	run.Main(env, qaRunArgs)

	gotSummary, err := qaFlaky("-archive-base-dir", dir, "-format", "json", "-show-aces=false")
	if err != nil {
		t.Fatal("Couldn't create summary", err)
	}

	summaries, ok := gotSummary.([]map[string](interface{}))
	require.Equal(t, true, ok, "Wrong type for summary: %T", gotSummary)

	require.Equal(t, 3, len(summaries), "Wrong number of flaky tests: %v\n%s\n%s", gotSummary, stderrBuf.String(), stdoutBuf.String())

	// NOTE(adamb) The values below are sensitive to class of exception raised *and*
	// source code for the line that created the failure.
	expectJson := `
[
	{
		"id": ["my-flaky-suite", ["Flaky", "flaky context"], "sometimes passes"],
		"total-count": 6,
		"pass-count": 1,
		"fail-count": 5,
		"count": {
			"fail:3d86ccd32dfd96821e98dccbd6db565d5ff6ffdc": 1,
			"fail:daa308628a41ad6c1a2b3f2dd3469489ff89943d": 2,
			"error:f2397994f7a7e0d86715ba03c8cd05a01816f6c7": 2,
			"pass": 1
		}
	},
  {
    "id": ["my-flaky-suite", ["MinitestFlakyTest"], "test_flaky"],
    "total-count": 6,
		"pass-count": 1,
		"fail-count": 5,
		"count": {
			"fail:466404515d7d0016850e29ea8cbffb16335da921": 1,
			"fail:28c9eed3b5efa356cb78ce4dd9a028507eb61e56": 2,
			"error:0c52dfdddef8fd1ef8fe3abfd429c2fa0174a4d7": 2,
			"pass": 1
		}
  },
  {
    "id": ["my-flaky-suite", ["TestUnitFlakyTest"], "test_flaky"],
    "total-count": 6,
		"pass-count": 1,
		"fail-count": 5,
		"count": {
			"fail:b8919726a12ab7672e38059f3b85cdbafbe8e87f": 1,
			"fail:7c7656d9c4de343894639afda9cee5db4b608230": 2,
			"error:6cffc4f3944405351816fc766014830e15058bde": 2,
			"pass": 1
		}
  }
]
`
	expectedSummary := []map[string](interface{}){}
	if err = json.Unmarshal([]byte(expectJson), &expectedSummary); err != nil {
		t.Fatal("Couldn't parse expected summary", err)
	}

	require.Equal(t, len(expectedSummary), len(summaries), "Wrong number of entries in summary: %v", summaries)

	for ix, expectedFields := range expectedSummary {
		gotFields := summaries[ix]
		if !assert.NotEqual(t, 0, len(gotFields), "Missing test %v, got %v", ix, summaries) {
			continue
		}

		for statId, expectedStat := range expectedFields {
			require.Equal(t, expectedStat, gotFields[statId], "Wrong summary for test %v, stat %v, got %v", ix, statId, gotFields)
		}
	}
}
