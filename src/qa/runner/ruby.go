package runner

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"qa/runners"
	"qa/tapj"
	"strings"

	"github.com/mattn/go-zglob"
)

type TestRunner interface {
	Run() ([]tapj.CaseEvent, tapj.TestEvent, error)
}

func RunAndDecodeTapjCmd(cmd *exec.Cmd, decodingCallbacks *tapj.DecodingCallbacks) error {
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error making stdout pipe.", err)
	}
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Error starting tests.", err)
	}

	err = tapj.Decoder{Reader: stdout}.Decode(decodingCallbacks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error decoding tests.", err)
	}

	return cmd.Wait()
}

type rubySingleTestRunner struct {
	rubyArgs []string
}

func (self rubySingleTestRunner) Run() ([]tapj.CaseEvent, tapj.TestEvent, error) {
	cmd := exec.Command("ruby", self.rubyArgs...)

	// First enumerate, accumulating suite, files and filters for each test to run
	var savedCases *[]tapj.CaseEvent
	var savedTest *tapj.TestEvent

	err := RunAndDecodeTapjCmd(cmd, &tapj.DecodingCallbacks{
		OnTest: func(cases []tapj.CaseEvent, test tapj.TestEvent) (err error) {
			if savedCases != nil || savedTest != nil {
				err = errors.New("Single test runner emitted multiple test events.")
				return
			}
			savedCases = &cases
			savedTest = &test
			return
		},
	})

	return *savedCases, *savedTest, err
}

type testRunnerEmitter interface {
	EmitTestRunners(seed int, file []string) ([]TestRunner, error)
}

type testUnitRunnerEmitter struct{}

func (self testUnitRunnerEmitter) EmitTestRunners(seed int, files []string) (testRunners []TestRunner, err error) {
	sharedData, _ := runners.Asset("ruby/shared.rb")
	var shared = string(sharedData)

	testUnitRunnerData, _ := runners.Asset("ruby/test-unit.rb")
	var testUnitRunner = string(testUnitRunnerData)

	seedArg := fmt.Sprintf("%v", seed)

	args := append(
		[]string{
			"-I", "lib", "-I", "test",
			"-e", shared,
			"-e", testUnitRunner,
			"--",
			"--dry-run",
			"--seed", seedArg,
			"--",
		},
		files...)
	err = RunAndDecodeTapjCmd(exec.Command("ruby", args...),
		&tapj.DecodingCallbacks{
			OnTest: func(cases []tapj.CaseEvent, test tapj.TestEvent) (err error) {
				testRunners = append(testRunners, rubySingleTestRunner{
					rubyArgs: []string{
						"-I", "lib", "-I", "test",
						"-e", shared,
						"-e", testUnitRunner,
						"--",
						"--seed", seedArg,
						"--name", test.Filter,
						"--",
						test.File,
					},
				})
				return
			},
		})
	return
}

type minitestRunnerEmitter struct{}

func (self minitestRunnerEmitter) EmitTestRunners(seed int, files []string) (testRunners []TestRunner, err error) {
	sharedData, _ := runners.Asset("ruby/shared.rb")
	var shared = string(sharedData)

	minitestRunnerData, _ := runners.Asset("ruby/minitest.rb")
	var minitestRunner = string(minitestRunnerData)

	seedArg := fmt.Sprintf("%v", seed)

	args := append(
		[]string{
			"-I", "lib", "-I", "test",
			"-e", shared,
			"-e", minitestRunner,
			"--",
			"--dry-run",
			"--seed", seedArg,
			"--",
		},
		files...)
	err = RunAndDecodeTapjCmd(exec.Command("ruby", args...),
		&tapj.DecodingCallbacks{
			OnTest: func(cases []tapj.CaseEvent, test tapj.TestEvent) (err error) {
				testRunners = append(testRunners, rubySingleTestRunner{
					rubyArgs: []string{
						"-I", "lib", "-I", "test",
						"-e", shared,
						"-e", minitestRunner,
						"--",
						"--seed", seedArg,
						"--name", test.Filter,
						"--",
						test.File,
					},
				})
				return
			},
		})

	return
}

type rspecRunnerEmitter struct{}

func (self rspecRunnerEmitter) EmitTestRunners(seed int, files []string) (testRunners []TestRunner, err error) {
	sharedData, _ := runners.Asset("ruby/shared.rb")
	var shared = string(sharedData)

	rspecRunnerData, _ := runners.Asset("ruby/rspec.rb")
	var rspecRunner = string(rspecRunnerData)

	seedArg := fmt.Sprintf("%v", seed)

	args := append(
		[]string{
			"-I", "lib", "-I", "spec",
			"-e", shared,
			"-e", rspecRunner,
			"--",
			"--dry-run",
			"--seed", seedArg,
			"--",
		},
		files...)
	err = RunAndDecodeTapjCmd(exec.Command("ruby", args...),
		&tapj.DecodingCallbacks{
			OnTest: func(cases []tapj.CaseEvent, test tapj.TestEvent) (err error) {
				testRunners = append(testRunners, rubySingleTestRunner{
					rubyArgs: []string{
						"-I", "lib", "-I", "spec",
						"-e", shared,
						"-e", rspecRunner,
						"--",
						"--seed", seedArg,
						test.Filter,
					},
				})
				return
			},
		})
	if err != nil {
		return
	}

	return
}

func chooseTestRunnerEmitter(filePath string, patterns map[string]testRunnerEmitter) (emitter testRunnerEmitter, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		for substring, e := range patterns {
			if strings.Contains(scanner.Text(), substring) {
				emitter = e
				return
			}
		}
	}

	return
}

func enumerateRunners(seed int, allFiles []string, patterns map[string]testRunnerEmitter) (testRunners []TestRunner, err error) {
	filesByTestRunEmitter := make(map[testRunnerEmitter][]string)

	for _, file := range allFiles {
		var emitter testRunnerEmitter
		emitter, err = chooseTestRunnerEmitter(file, patterns)
		if err != nil {
			return
		}

		if emitter != nil {
			filesByTestRunEmitter[emitter] = append(filesByTestRunEmitter[emitter], file)
		}
	}

	for emitter, files := range filesByTestRunEmitter {
		var runners []TestRunner
		fmt.Fprintln(os.Stderr, "Emitting test runners for files:", files)
		runners, err = emitter.EmitTestRunners(seed, files)
		if err != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "Found %d runners\n", len(runners))

		testRunners = append(testRunners, runners...)
	}

	return
}

func EnumerateTestRunners(seed int) (runners []TestRunner, err error) {
	// TODO(adamb) Should run ruby command that dumps which versions of minitest, rspec, test-unit
	//     are present, for use by detection logic.

	var testMatches []string
	m, _ := zglob.Glob("test/**/test_*.rb")
	testMatches = append(testMatches, m...)
	m, _ = zglob.Glob("test/**/test-*.rb")
	testMatches = append(testMatches, m...)
	m, _ = zglob.Glob("test/**/*-test.rb")
	testMatches = append(testMatches, m...)
	m, _ = zglob.Glob("test/**/*_test.rb")
	testMatches = append(testMatches, m...)

	if len(testMatches) > 0 {
		testPatterns := make(map[string]testRunnerEmitter)
		testPatterns["Minitest::Test"] = minitestRunnerEmitter{}
		testPatterns["Test::Unit"] = testUnitRunnerEmitter{}
		testPatterns["SecuredocsUnitTestCase"] = testUnitRunnerEmitter{}

		fmt.Fprintln(os.Stderr, "Found tests:", testMatches)
		var testRunners []TestRunner
		testRunners, err = enumerateRunners(seed, testMatches, testPatterns)
		if err != nil {
			return
		}

		runners = append(runners, testRunners...)
	}

	specMatches, _ := zglob.Glob("spec/**/*_spec.rb")
	if len(specMatches) > 0 {
		fmt.Fprintln(os.Stderr, "Found specs:", specMatches)

		specPatterns := make(map[string]testRunnerEmitter)
		specPatterns["RSpec"] = rspecRunnerEmitter{}
		specPatterns["describe"] = rspecRunnerEmitter{}
		var specRunners []TestRunner
		specRunners, err = enumerateRunners(seed, specMatches, specPatterns)
		if err != nil {
			return
		}

		runners = append(runners, specRunners...)
	}

	return
}
