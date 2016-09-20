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
	err := flaky.Main(env, append([]string{"flaky"}, args...))
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

	qaRunArgv := []string{
		"qa run",
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
	run.Main(env, qaRunArgv)
	run.Main(env, qaRunArgv)
	env.Vars["QA_FLAKY_TYPE"] = "assert"
	// Expect one flaky (assertion) fail for each test type
	run.Main(env, qaRunArgv)
	env.Vars["QA_FLAKY_1"] = "false"
	env.Vars["QA_FLAKY_2"] = "false"
	// Expect this entire run to pass.
	run.Main(env, qaRunArgv)
	env.Vars["QA_FLAKY_1"] = "false"
	env.Vars["QA_FLAKY_2"] = "true"
	// Expect a different kind of flaky (assertion) fail for each test type
	run.Main(env, qaRunArgv)
	qaRunArgv[4] = "r2"
	// Expect a failure similar to above, but still counted with r1.
	run.Main(env, qaRunArgv)

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
			"fail:ed39cd64354ca00d72c29799fc9bc013c2b62455": 1,
			"fail:009c8245aeb284a57864c957947418f58296b3d4": 2,
			"error:498e0db89422d016523f5309e55300f07642ae4f": 2,
			"pass": 1
		}
  },
  {
    "id": ["my-flaky-suite", ["TestUnitFlakyTest"], "test_flaky"],
    "total-count": 6,
		"pass-count": 1,
		"fail-count": 5,
		"count": {
			"fail:5789b8d214aa8d3152268191683010c1dc6da2ad": 1,
			"fail:35c14e60e327c7c555d36a8b8a17aec094a31586": 2,
			"error:87f44f5622552a0c88645dc1c479bf72bccff65f": 2,
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
