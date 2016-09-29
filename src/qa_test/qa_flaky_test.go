package qa_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"qa/cmd"
	"qa/cmd/flaky"
	"qa/cmd/run"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qaFlakyRepro(dir string, vars map[string]string, stdin io.Reader, args ...string) (string, error) {
	var out bytes.Buffer
	env := &cmd.Env{Stdin: stdin, Stdout: &out, Stderr: &out, Vars: vars, Dir: dir}
	err := flaky.Main(env, append([]string{"flaky"}, args...))
	if err != nil {
		return "", fmt.Errorf("error running with %v (%v): %v", args, err, out.String())
	}

	return out.String(), nil
}

func qaFlaky(args ...string) ([]map[string]interface{}, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	env := &cmd.Env{Stdout: &stdout, Stderr: &stderr}
	err := flaky.Main(env, append([]string{"flaky"}, args...))
	if err != nil {
		return nil, fmt.Errorf("error running with %v (%v): %v", args, err, stderr.String())
	}

	decoder := json.NewDecoder(bytes.NewBuffer(stdout.Bytes()))
	var summaries []map[string]interface{}
	for {
		summary := map[string](interface{}){}
		if err = decoder.Decode(&summary); err != nil {
			if err == io.EOF {
				break
			} else {
				return summaries, fmt.Errorf("error parsing JSON (%v): %v", err, stdout.String())
			}
		} else {
			summaries = append(summaries, summary)
		}
	}

	return summaries, nil
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
		"-archive", dir,
		"-pretty-overwrite=false",
		"-pretty-quiet-pass=false",
		"-pretty-quiet-omit=false",
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

	summaries, err := qaFlaky("-archive", dir, "top", "--format", "json")
	if err != nil {
		t.Fatal("Couldn't create summary", err)
	}

	require.Equal(t, 3, len(summaries),
		"Wrong number of flaky tests: %v\n%s\n%s", summaries, stderrBuf.String(), stdoutBuf.String())

	// NOTE(adamb) The values below are sensitive to class of exception raised *and*
	// source code for the line that created the failure.
	expectJson := `
[
	{
		"id": ["my-flaky-suite", ["TestUnitFlakyTest"], "test_flaky"],
		"total-count": 7,
		"pass-count": 1,
		"fail-count": 6,
		"probability": {
			"fail:bce2d9e87edf0795ba31d2b9e028cd9bf4b8cbd8": 0.2222222222222222,
			"fail:5be898122b15f5ada2f0a9d36849a88baee95759": 0.3333333333333333,
			"error:19ff195c94b178e2dccc45a51664364969375c31": 0.4444444444444444,
			"pass": 0.2222222222222222
		},
		"repro-limit-probability": {
			"fail:bce2d9e87edf0795ba31d2b9e028cd9bf4b8cbd8": 0.999,
			"fail:5be898122b15f5ada2f0a9d36849a88baee95759": 0.999,
			"error:19ff195c94b178e2dccc45a51664364969375c31": 0.999,
			"pass": 0.999
		},
		"repro-run-limit": {
			"fail:bce2d9e87edf0795ba31d2b9e028cd9bf4b8cbd8": 28,
			"fail:5be898122b15f5ada2f0a9d36849a88baee95759": 18,
			"error:19ff195c94b178e2dccc45a51664364969375c31": 12,
			"pass": 28
		},
		"count": {
			"fail:bce2d9e87edf0795ba31d2b9e028cd9bf4b8cbd8": 1,
			"fail:5be898122b15f5ada2f0a9d36849a88baee95759": 2,
			"error:19ff195c94b178e2dccc45a51664364969375c31": 3,
			"pass": 1
		}
	},
  {
    "id": ["my-flaky-suite", ["MinitestFlakyTest"], "test_flaky"],
    "total-count": 7,
		"pass-count": 1,
		"fail-count": 6,
		"count": {
			"fail:844eec49198f1407933bcb08a6c07e65f5955648": 1,
			"fail:4833814947d4d1e4335f4e0c1af838b8fac893c0": 2,
			"error:faeff05b0b228914d7a6b0f3a81aa93c9f9cc882": 3,
			"pass": 1
		}
	},
	{
		"id": ["my-flaky-suite", ["Flaky", "flaky context"], "sometimes passes"],
		"total-count": 7,
		"pass-count": 1,
		"fail-count": 6,
		"count": {
			"fail:26c7cd9613c132da5c2cc050f72ce4228cc7354c": 1,
			"fail:e454bef3be82a9db32a1639f484a7600c12ca2ee": 2,
			"error:4a2add88a90bca8e442977b4b1f5b00ca558f46b": 3,
			"pass": 1
		}
  }
]
`

	expectedSummary := []map[string](interface{}){}
	if err = json.Unmarshal([]byte(expectJson), &expectedSummary); err != nil {
		t.Fatal("Couldn't parse expected summary", err)
	}

	assert.Equal(t, len(expectedSummary), len(summaries), "Wrong number of entries in summary: %v", summaries)

	for ix, expectedFields := range expectedSummary {
		gotFields := summaries[ix]
		if !assert.NotEqual(t, 0, len(gotFields), "Missing test %v, got %v", ix, summaries) {
			continue
		}

		for statId, expectedStat := range expectedFields {
			assert.Equal(t, expectedStat, gotFields[statId], "Wrong summary for test %v, stat %v, got %v", ix, statId, gotFields)
		}
	}

	env.Vars["QA_FLAKY_1"] = "true"
	env.Vars["QA_FLAKY_2"] = "false"
	env.Vars["QA_FLAKY_TYPE"] = "error"

	testRepl(
		t,
		[]string{"__method__", "@state"},
		[]string{":test_flaky", ":post_setup"},
		func (stdin io.Reader) (string, error) {
			return qaFlakyRepro(
				env.Dir,
				env.Vars,
				stdin,
				"-archive", dir, "repro", "1a")
		},
	)

	testRepl(
		t,
		[]string{"__method__", "@state"},
		[]string{":test_flaky", ":post_setup"},
		func (stdin io.Reader) (string, error) {
			return qaFlakyRepro(
				env.Dir,
				env.Vars,
				stdin,
				"-archive", dir, "repro", "2a")
		},
	)

	testRepl(
		t,
		[]string{"example.description", "state"},
		[]string{`"sometimes passes"`, ":post_before"},
		func (stdin io.Reader) (string, error) {
			return qaFlakyRepro(
				env.Dir,
				env.Vars,
				stdin,
				"-archive", dir, "repro", "3a")
		},
	)
}

var ansiRegexp = regexp.MustCompile("[\u001b\u009b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]")
func testRepl(t *testing.T, expressions []string, expectResults []string, runRepl func(io.Reader) (string, error)) {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	require.NoError(t, err, "Couldn't generate nonce for testRepl")
	quotedNonce := fmt.Sprintf(`"%x"`, b)

	code := fmt.Sprintf("[%s,%s]\n", quotedNonce, strings.Join(expressions, ","))

	out, err := runRepl(bytes.NewBuffer([]byte(code)))
	require.NoError(t, err, "Problem running repl with code %s", code)
	out = ansiRegexp.ReplaceAllString(out, "")

	expectResults = append([]string{quotedNonce}, expectResults...)
	require.Regexp(t,
		regexp.MustCompile(fmt.Sprintf(`\[\s*%s\s*\]`, strings.Join(expectResults, ",\\s*"))),
		out,
	)
}
