package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"qa/tapjio"
	"sort"
	"strings"
	"sync"

	"github.com/mattn/go-zglob"
)

//go:generate go-bindata -o $GOGENPATH/qa/runner/assets/bindata.go -pkg assets -prefix ../runner-assets/ ../runner-assets/...

type TestRunner interface {
	Run(env map[string]string, visitor tapjio.Visitor) error
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

type FileGlob struct {
	dir      string
	patterns []string
}

func NewFileGlob(dir string, patterns []string) *FileGlob {
	return &FileGlob{dir: dir, patterns: patterns}
}

type FileLister interface {
	Patterns() []string
	Dir() string
	ListFiles() ([]string, error)
}

func (f *FileGlob) Dir() string {
	return f.dir
}

func (f *FileGlob) Patterns() []string {
	return f.patterns
}

func (f *FileGlob) ListFiles() ([]string, error) {
	var files []string
	dir := f.dir
	for _, pattern := range f.patterns {
		// Make glob absolute, using dir
		relative := !filepath.IsAbs(pattern)
		if relative && dir != "" {
			pattern = filepath.Join(dir, pattern)
		}

		// Expand glob
		globFiles, err := zglob.Glob(pattern)
		if err != nil {
			return files, err
		}

		// Strip prefix from glob matches if needed.
		if relative && dir != "" {
			trimPrefix := fmt.Sprintf("%s%c", dir, os.PathSeparator)
			for _, file := range globFiles {
				files = append(files, strings.TrimPrefix(file, trimPrefix))
			}
		} else {
			files = append(files, globFiles...)
		}
	}

	return files, nil
}

type SquashPolicy int

const (
	SquashNothing SquashPolicy = iota
	SquashByFile
	SquashAll
)

type Config struct {
	Name              string
	FileLister        FileLister
	PassthroughConfig map[string](interface{})
	Dir               string
	EnvVars           map[string]string
	Seed              int
	SquashPolicy      SquashPolicy
	TraceProbes       []string
}

func (f *Config) Files() ([]string, error) {
	return f.FileLister.ListFiles()
}

type Context interface {
	EnumerateRunners() ([]tapjio.TraceEvent, []TestRunner, error)
	Close() error
}

type eventUnion struct {
	trace  *tapjio.TraceEvent
	begin  *tapjio.TestStartedEvent
	finish *tapjio.TestEvent
	error  error
}

func RunAll(
	visitor tapjio.Visitor,
	workerEnvs []map[string]string,
	tally *tapjio.ResultTally,
	runners []TestRunner) (err error) {

	numWorkers := len(workerEnvs)

	var testRunnerChan = make(chan TestRunner, numWorkers)

	// Enqueue each testRunner on testRunnerChan
	go func() {
		// Sort runners by test count. This heuristic helps our workers avoid being idle
		// near the end of the run by running testRunners with the most tests first, avoiding
		// scenarios where the last testRunner we run has many tests, causing the entire test
		// run to drag on needlessly while other workers are idle.
		By(func(r1, r2 *TestRunner) bool { return (*r2).TestCount() < (*r1).TestCount() }).Sort(runners)

		for _, testRunner := range runners {
			testRunnerChan <- testRunner
		}
		close(testRunnerChan)
	}()

	var quitChan = make(chan struct{})
	var eventChan = make(chan eventUnion, numWorkers)

	var awaitJobs sync.WaitGroup
	awaitJobs.Add(numWorkers)

	for _, workerEnv := range workerEnvs {
		env := workerEnv
		go func() {
			defer awaitJobs.Done()
			for testRunner := range testRunnerChan {
				select {
				case <-quitChan:
					for i := testRunner.TestCount(); i > 0; i-- {
						eventChan <- eventUnion{error: errors.New("already aborted")}
					}
					continue
				default:
				}

				err := testRunner.Run(
					env,
					&tapjio.DecodingCallbacks{
						OnTestBegin: func(test tapjio.TestStartedEvent) error {
							eventChan <- eventUnion{nil, &test, nil, nil}
							return nil
						},
						OnTest: func(test tapjio.TestEvent) error {
							eventChan <- eventUnion{nil, nil, &test, nil}
							return nil
						},
						OnTrace: func(trace tapjio.TraceEvent) error {
							eventChan <- eventUnion{&trace, nil, nil, nil}
							return nil
						},
					})

				if err != nil {
					eventChan <- eventUnion{nil, nil, nil, err}
				}
			}
		}()
	}

	go func() {
		awaitJobs.Wait()
		close(eventChan)
	}()

	for eventUnion := range eventChan {
		if eventUnion.trace != nil {
			err = visitor.TraceEvent(*eventUnion.trace)
			if err != nil {
				return
			}
			continue
		}

		begin := eventUnion.begin
		if begin != nil {
			err = visitor.TestStarted(*begin)
			if err != nil {
				return
			}
			continue
		}

		test := eventUnion.finish

		if eventUnion.error != nil {
			test = &tapjio.TestEvent{
				Type:   "test",
				Time:   0,
				Label:  "<internal error: " + eventUnion.error.Error() + ">",
				Status: tapjio.Error,
				Exception: &tapjio.TestException{
					Message: eventUnion.error.Error(),
				},
			}
		}

		tally.Increment(test.Status)

		err = visitor.TestFinished(*test)
		if err != nil {
			return
		}
	}

	return
}
