package qa_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"qa/cmd"
	"qa/cmd/discover"
	"qa/cmd/grouping"
	"qa/cmd/summary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeJsonLines(r io.Reader) ([]interface{}, error) {
	objs := [](interface{}){}
	decoder := json.NewDecoder(r)
	for {
		var obj interface{}
		if err := decoder.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			return objs, err
		}
		objs = append(objs, obj)
	}

	return objs, nil
}

func qaGrouping(input []interface{}, args ...string) ([]interface{}, error) {
	var stdin bytes.Buffer
	encoder := json.NewEncoder(&stdin)
	for _, obj := range input {
		encoder.Encode(obj)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error
	env := &cmd.Env{Stdout: &stdout, Stderr: &stderr, Stdin: bytes.NewBuffer(stdin.Bytes())}
	if err = grouping.Main(env, args); err != nil {
		return nil, fmt.Errorf("error running with %v (%v): %v", args, err, stderr.String())
	}

	var got [](interface{})
	got, err = decodeJsonLines(bytes.NewBuffer(stdout.Bytes()))
	if err != nil {
		return nil, err
	}

	return got, nil
}

func checkGrouping(t *testing.T, message string, expect []interface{}, input []interface{}, args ...string) {
	var got [](interface{})
	var err error
	got, err = qaGrouping(input, args...)
	if err != nil {
		t.Fatal("Couldn't run filter", err)
	}

	require.Equal(t, got, expect, message)
}

const eventsJson = `{"str": "string", "id": "0"}
{"str": "string", "id": 1, "one": 1}
{"str": "string", "id": 2, "one": 1, "composite": {"a": [1]}}
{"str": "string", "id": 3.1, "one": 1, "str2": "string", "composite": {}}
{"str": "string", "id": 4, "one": 1, "composite": {"a": [1]}}
{"str": "string", "id": 5, "one": false}
`

// test collapse-id & keep-if-any
func TestKeepIfAny(t *testing.T) {
	var events [](interface{})
	var err error

	events, err = decodeJsonLines(bytes.NewBufferString(eventsJson))
	if err != nil {
		t.Fatal("Couldn't parse events JSON", err)
	}

	checkGrouping(t, "String values should work",
		events,
		events, "--collapse-id", "id", "--keep-if-any", "str==string")

	// Note that we look for id==0, even though it's "id": "0" above.
	checkGrouping(t, "Integers should be matched exactly (not as strings)",
		events[5:6],
		events, "--collapse-id", "id", "--keep-if-any", "id==0", "--keep-if-any", "id==5")

	checkGrouping(t, "Integer values and no-tolerate-nil should work",
		events[1:],
		events, "--collapse-id", "one", "--no-tolerate-nil", "--keep-if-any", "id==1", "--keep-if-any", "id==5")

	checkGrouping(t, "Decimal values should work",
		events[3:4],
		events, "--collapse-id", "id", "--keep-if-any", "id==3.1")

	checkGrouping(t, "Should use deep equals for --collapse-id",
		[]interface{}{events[2], events[4]},
		events, "--collapse-id", "composite", "--keep-if-any", "id==2")

	checkGrouping(t, "Missing ids should work for --collapse-id",
		[]interface{}{events[0], events[1], events[5]},
		events, "--collapse-id", "composite", "--keep-if-any", "id==1")

	checkGrouping(t, "Test composites work in --keep-if-any",
		[]interface{}{events[2], events[4]},
		events, "--collapse-id", "composite", "--keep-if-any", "composite=={\"a\":[1]}")
}

// test collapse-id & keep-if-any & secondary-whitelist
func TestSecondaryWhitelist(t *testing.T) {
	eventsBytes := []byte(eventsJson)

	var events [](interface{})
	var err error

	events, err = decodeJsonLines(bytes.NewBuffer(eventsBytes))
	if err != nil {
		t.Fatal("Couldn't parse events JSON", err)
	}

	checkGrouping(t, "Exercise --keep-residual-records-matching-kept",
		[]interface{}{events[4], events[2]},
		events, "--collapse-id", "id", "--keep-if-any", "id==4",
		"--keep-residual-records-matching-kept", "composite,one")

	checkGrouping(t, "Nested keys should work for --keep-residual-records-matching-kept",
		[]interface{}{events[3], events[4], events[1], events[2]},
		events, "--collapse-id", "id", "--keep-if-any", "id==4", "--keep-if-any", "id==3.1",
		"--keep-residual-records-matching-kept", "composite.a,one")
}

func qaDiscover(args ...string) ([]interface{}, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error
	env := &cmd.Env{Stdout: &stdout, Stderr: &stderr}
	if err = discover.Main(env, args); err != nil {
		return nil, fmt.Errorf("error running with %v (%v): %v", args, err, stderr.String())
	}

	var got [](interface{})
	got, err = decodeJsonLines(bytes.NewBuffer(stdout.Bytes()))
	if err != nil {
		return nil, err
	}

	return got, nil
}

func qaSummary(input []interface{}, args ...string) ([]map[string](interface{}), error) {
	var stdin bytes.Buffer
	encoder := json.NewEncoder(&stdin)
	for _, obj := range input {
		encoder.Encode(obj)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error
	env := &cmd.Env{Stdout: &stdout, Stderr: &stderr, Stdin: bytes.NewBuffer(stdin.Bytes())}
	if err = summary.Main(env, args); err != nil {
		return nil, fmt.Errorf("error running with %v (%v): %v", args, err, stderr.String())
	}

	summary := []map[string](interface{}){}
	if err = json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		return summary, fmt.Errorf("error parsing JSON (%v): %v", err, stdout.String())
	}

	return summary, nil
}

// Test that our summary math is correct
func TestSummary(t *testing.T) {
	// Sample data properties:
	//
	// Run on 1/27/14:
	// Suite run 1: tools/review/node-testruns/ci:testrun+macosx+x86_64
	//   TestSimpleBuilds:test_simple_failing_build passes
	//   TestSimpleBuilds:test_simple_passing_build passes
	//   TestSimpleBuilds:test_simple_passing_build passes
	//   TestAnnotations:test_annotations passes
	//   TestMerges:test_slave_interval_scheduler passes
	// Suite run 2: tools/review/node-testruns/ci:testrun+macosx+x86_64
	//   TestSimpleBuilds:test_simple_failing_build passes
	//   TestSimpleBuilds:test_simple_passing_build passes
	//   TestSimpleBuilds:test_simple_passing_build passes
	//   TestAnnotations:test_annotations error due to timeout
	//   TestMerges:test_slave_interval_scheduler fails
	//
	// Run on 1/20/14:
	// Suite run 3: platform/spin/backend/identity/testrun:testrun+macosx+x86_64
	//   RegistrationTest:test_registration passes
	//   RegistrationTest:test_remembering passes
	//   ConversationCloseTest:test_esc_close passes
	// Suite run attempt that left an Empty Tapj file
	// Suite run 4: platform/spin/backend/identity/testrun:testrun+macosx+x86_64
	//   RegistrationTest:test_registration passes
	//   RegistrationTest:test_remembering passes
	//   ConversationCloseTest:test_esc_close error

	const untilDate = "2014-01-27"
	// NOTE(tim) tapj-collect uses directory names to determine when tests were
	// run, not the timestamps found in the tapj file name or output. This sample
	// data mimics old data by using a directory labelled from a date in the past.

	// Going back 7 days from 1/27 should only include test results from 1/27
	results, err := qaDiscover("--number-days", "7", "--until-date", untilDate, "--dir", "fixtures/discover")
	if err != nil {
		t.Fatal("Couldn't discover results", err)
	}

	require.Equal(t, 10, len(results),
		fmt.Sprintf("Found wrong number of results: %v", results))

	// Going back 8 days from 1/27 should include all test results.
	// Test results from 1/20 include an empty tapj file to test
	// that tapj-collect does not blow up when it encounters one
	results, err = qaDiscover("--number-days", "8", "--until-date", untilDate, "--dir", "fixtures/discover")
	if err != nil {
		t.Fatal("Couldn't discover results", err)
	}

	expectJson := `
[
	{
		"id": ["platform/spin/backend/identity/testrun:testrun+macosx+x86_64",["RegistrationTest"], "test_registration"],
		"mean": {"pass": 74.846147},
		"median": {"pass": 74.846147},
		"std_dev": {"pass": 22.855770061885902},
		"count": {"pass": 2},
		"total_count": 2, "pass_count": 2, "fail_count": 0
	},
	{
		"id": ["platform/spin/backend/identity/testrun:testrun+macosx+x86_64",["RegistrationTest"], "test_remembering"],
		"mean": {"pass": 34.0779615},
		"median": {"pass": 34.0779615},
		"std_dev": {"pass": 0.8973050702968839},
		"count": {"pass": 2},
		"total_count": 2, "pass_count": 2, "fail_count": 0
	},
	{
		"id": ["tools/review/node-testruns/ci:testrun+macosx+x86_64",["TestSimpleBuilds"], "test_simple_failing_build"],
		"mean": {"pass": 64.05913749999999},
		"median": {"pass": 64.05913749999999},
		"std_dev": {"pass": 23.171006043113454},
		"count": {"pass": 2},
		"total_count": 2, "pass_count": 2, "fail_count": 0
	},
	{
		"id": ["tools/review/node-testruns/ci:testrun+macosx+x86_64",["TestSimpleBuilds"], "test_simple_passing_build"],
		"mean": {"pass": 19.76621775},
		"median": {"pass": 21.482759},
		"std_dev": {"pass": 4.154955093996914},
		"count": {"pass": 4},
		"total_count": 4, "pass_count": 4, "fail_count": 0
	},
	{
	"id": ["platform/spin/backend/identity/testrun:testrun+macosx+x86_64",["ConversationCloseTest"], "test_esc_close"],
		"mean": {"pass": 15.123134, "error:7c31715b7a768bfc43f8a604d0361ace35f08835": 0.005918},
		"median": {"pass": 15.123134, "error:7c31715b7a768bfc43f8a604d0361ace35f08835": 0.005918},
		"std_dev": {"pass": 0.0, "error:7c31715b7a768bfc43f8a604d0361ace35f08835": 0.0},
		"count": {"pass": 1, "error:7c31715b7a768bfc43f8a604d0361ace35f08835": 1},
		"total_count": 2, "pass_count": 1, "fail_count": 1
	},
	{
	"id": ["tools/review/node-testruns/ci:testrun+macosx+x86_64",["TestAnnotations"], "test_annotations"],
		"mean": {"pass": 55.69333, "error:609635f9467fd88c85143d469a04654025fcdd00": 1080.0},
		"median": {"pass": 55.69333, "error:609635f9467fd88c85143d469a04654025fcdd00": 1080.0},
		"std_dev": {"pass": 0.0, "error:609635f9467fd88c85143d469a04654025fcdd00": 0.0},
		"count": {"pass": 1, "error:609635f9467fd88c85143d469a04654025fcdd00": 1},
		"total_count": 2, "pass_count": 1, "fail_count": 1
	},
	{
		"id": ["tools/review/node-testruns/ci:testrun+macosx+x86_64",["TestMerges"], "test_slave_interval_scheduler"],
		"mean": {"pass": 11.234545, "fail:d1fff5ccf36ecee47e03ec0048a0a3b0c8758f5a": 165.107976},
		"median": {"pass": 11.234545, "fail:d1fff5ccf36ecee47e03ec0048a0a3b0c8758f5a": 165.107976},
		"std_dev": {"pass": 0.0, "fail:d1fff5ccf36ecee47e03ec0048a0a3b0c8758f5a": 0.0},
		"count": {"pass": 1, "fail:d1fff5ccf36ecee47e03ec0048a0a3b0c8758f5a": 1},
		"total_count": 2, "pass_count": 1, "fail_count": 1
	}
]
`
	expectedSummary := []map[string](interface{}){}
	if err = json.Unmarshal([]byte(expectJson), &expectedSummary); err != nil {
		t.Fatal("Couldn't parse expected summary", err)
	}

	collapseId := "suite.label,case-labels,label"
	gotSummary, err := qaSummary(results,
		"--format", "json",
		"--show-aces",
		"--duration", "time",
		"--sort-by", "suite.start",
		"--group-by", collapseId,
		"--subgroup-by", "outcome-digest",
		"--ignore-if", "status==\"skip\"",
		"--ignore-if", "status==\"omit\"",
		"--success-if", "status==\"pass\"")
	if err != nil {
		t.Fatal("Couldn't create summary", err)
	}

	require.Equal(t, len(expectedSummary), len(gotSummary), "Wrong number of entries in summary")

	for ix, expectedStats := range expectedSummary {
		gotStats := gotSummary[ix]
		if !assert.NotEqual(t, 0, len(gotStats), "Missing test %v, got %v", ix, gotSummary) {
			continue
		}

		for statId, expectedStat := range expectedStats {
			require.Equal(t, expectedStat, gotStats[statId], "Wrong summary for test %v, stat %v, got %v", ix, statId, gotStats)
		}
	}
}
