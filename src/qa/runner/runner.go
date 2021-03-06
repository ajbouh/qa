package runner

import (
	"errors"
	"qa/glob"
	"qa/tapjio"
	"sort"
	"sync"
)

//go:generate go-bindata -o $GOGENPATH/qa/runner/assets/bindata.go -pkg assets -prefix ../runner-assets/ ../runner-assets/...

type TestDependencyEntry struct {
	Label        string
	File         tapjio.FilePath
	Filter       tapjio.TestFilter
	Dependencies tapjio.TestDependencies
}

type TestRunner interface {
	Run(env map[string]string, seed int, visitor tapjio.Visitor) error
	Dependencies() []TestDependencyEntry
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
	return &FileGlob{dir: dir, patterns: append([]string{}, patterns...)}
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
	for _, pattern := range f.patterns {
		// Expand glob
		globFiles, err := glob.Glob(f.dir, pattern)
		if err != nil {
			return files, err
		}

		files = append(files, globFiles...)
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
	SquashPolicy      SquashPolicy
	TraceProbes       []string
	Filters           []tapjio.TestFilter
}

func (f *Config) Files() ([]string, error) {
	return f.FileLister.ListFiles()
}

type Context interface {
	EnumerateRunners(seed int) ([]tapjio.TraceEvent, []TestRunner, error)
	Close() error
}

type eventUnion struct {
	trace  *tapjio.TraceEvent
	begin  *tapjio.TestBeginEvent
	finish *tapjio.TestFinishEvent
	await  *tapjio.AwaitAttachEvent
	error  error
}

func RunAll(
	visitor tapjio.Visitor,
	workerEnvs []map[string]string,
	tally *tapjio.ResultTally,
	seed int,
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
					seed,
					&tapjio.DecodingCallbacks{
						OnTestBegin: func(test tapjio.TestBeginEvent) error {
							eventChan <- eventUnion{begin: &test}
							return nil
						},
						OnTestFinish: func(test tapjio.TestFinishEvent) error {
							eventChan <- eventUnion{finish: &test}
							return nil
						},
						OnAwaitAttach: func(event tapjio.AwaitAttachEvent) error {
							eventChan <- eventUnion{await: &event}
							return nil
						},
						OnTrace: func(trace tapjio.TraceEvent) error {
							eventChan <- eventUnion{trace: &trace}
							return nil
						},
					})

				if err != nil {
					eventChan <- eventUnion{error: err}
				}
			}
		}()
	}

	go func() {
		awaitJobs.Wait()
		close(eventChan)
	}()

	for eventUnion := range eventChan {
		if eventUnion.await != nil {
			err = visitor.AwaitAttach(*eventUnion.await)
			if err != nil {
				return
			}
			continue
		}

		if eventUnion.trace != nil {
			err = visitor.TraceEvent(*eventUnion.trace)
			if err != nil {
				return
			}
			continue
		}

		begin := eventUnion.begin
		if begin != nil {
			err = visitor.TestBegin(*begin)
			if err != nil {
				return
			}
			continue
		}

		test := eventUnion.finish

		if eventUnion.error != nil {
			test = &tapjio.TestFinishEvent{
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

		err = visitor.TestFinish(*test)
		if err != nil {
			return
		}
	}

	return
}
