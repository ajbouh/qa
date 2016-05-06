package runner

import (
	"qa/tapjio"
	"sort"
)

//go:generate go-bindata -o $GOGENPATH/qa/runner/assets/bindata.go -pkg assets -prefix ../runner-assets/ ../runner-assets/...

type TestRunner interface {
	Run(env map[string]string, callbacks tapjio.DecodingCallbacks) error
	TestCount() int
}

type By func(r1, r2 *TestRunner) bool

func (s By) Sort(runners []TestRunner) {
	trs := &testRunnerSorter{runners: runners, by: s}
	sort.Sort(trs)
}

type testRunnerSorter struct {
	runners []TestRunner
	by      func(r1, r2 *TestRunner) bool
}

func (s *testRunnerSorter) Len() int {
	return len(s.runners)
}

func (s *testRunnerSorter) Swap(i, j int) {
	s.runners[i], s.runners[j] = s.runners[j], s.runners[i]
}

func (s *testRunnerSorter) Less(i, j int) bool {
	return s.by(&s.runners[i], &s.runners[j])
}
