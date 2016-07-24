package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"qa/tapjio"
	"sort"
	"strings"

	"github.com/mattn/go-zglob"
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
