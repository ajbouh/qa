package qa_test

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"qa/cmd/run"
	"qa/tapjio"
	"qa_test/testutil"
	"testing"

	// "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qaWatch(quitCh chan struct{}, c chan<- *testutil.Transcript, dir string, args ...string) error {
	defer close(c)

	var transcriptVisitor tapjio.Visitor
	var transcript *testutil.Transcript
	visitor := &tapjio.DecodingCallbacks{
		OnSuiteBegin: func(event tapjio.SuiteBeginEvent) error {
			if transcript == nil {
				transcript, transcriptVisitor = testutil.NewTranscriptBuilder()
			}
			return transcriptVisitor.SuiteBegin(event)
		},
		OnTestBegin: func(event tapjio.TestBeginEvent) error {
			return transcriptVisitor.TestBegin(event)
		},
		OnTestFinish: func(event tapjio.TestFinishEvent) error {
			return transcriptVisitor.TestFinish(event)
		},
		OnTrace: func(event tapjio.TraceEvent) error {
			if transcript == nil {
				transcript, transcriptVisitor = testutil.NewTranscriptBuilder()
			}
			return transcriptVisitor.TraceEvent(event)
		},
		OnSuiteFinish: func(event tapjio.SuiteFinishEvent) error {
			err := transcriptVisitor.SuiteFinish(event)
			c <- transcript
			transcript = nil
			return err
		},
	}

	rd, wr := io.Pipe()
	go func() {
		<-quitCh
		wr.Close()
	}()
	_, err := testutil.RunQaCmd(run.Main, visitor, rd, dir, args)
	return err
}

func removeFile(t *testing.T, dir string, subpath string) {
	path := filepath.Join(dir, subpath)
	require.NoError(t, os.Remove(path), "Couldn't remove file")
}

func removeDir(t *testing.T, dir, subpath string) {
	writeFile(t, dir, ".git/index.lock", "")

	path := filepath.Join(dir, subpath)
	require.NoError(t, os.RemoveAll(path), "Couldn't remove file")

	removeFile(t, dir, ".git/index.lock")
}

func removeFiles(t *testing.T, dir string, subpaths ...string) {
	writeFile(t, dir, ".git/index.lock", "")

	for _, subpath := range subpaths {
		path := filepath.Join(dir, subpath)
		require.NoError(t, os.Remove(path), "Couldn't remove file")
	}

	removeFile(t, dir, ".git/index.lock")
}

func renameFile(t *testing.T, src, dst string) {
	require.NoError(t, os.Rename(src, dst), "Couldn't rename file")
}

func writeFile(t *testing.T, dir, basename, content string) {
	name := filepath.Join(dir, basename)
	parent := filepath.Dir(name)

	if _, err := os.Stat(parent); os.IsNotExist(err) {
		err = os.MkdirAll(parent, os.FileMode(0755))
		require.NoError(t, err, "Couldn't mkdir parent to write file")
	}

	// Write file to temp location so file watcher doesn't see file half-written
	tempName := name + ".tmp"
	err := ioutil.WriteFile(tempName, []byte(content), os.FileMode(0644))
	require.NoError(t, err, "Couldn't write file")
	require.NoError(t, os.Rename(tempName, name), "Couldn't rename file after writing")
}

func awaitResult(t *testing.T, c chan *testutil.Transcript, expectStatuses map[string]tapjio.Status) {
	transcript, ok := <-c
	require.Equal(t, true, ok, "Transcript channel should not be closed.")

	gotStatuses := map[string]tapjio.Status{}
	for _, event := range transcript.TestFinishEvents {
		gotStatuses[event.Label] = event.Status
	}

	require.Equal(t, expectStatuses, gotStatuses, "Wrong statuses")
}

func writeFileAndAwaitResult(t *testing.T, dir, path, source string, c chan *testutil.Transcript, expectStatuses map[string]tapjio.Status) {
	writeFile(t, dir, path, source)
	awaitResult(t, c, expectStatuses)
}

func TestWatch(t *testing.T) {
	dir, err := ioutil.TempDir("", "qa-watch-tests")
	require.NoError(t, err, "Couldn't make temporary directory")

	defer os.RemoveAll(dir)

	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err, "Couldn't eval symlinks for temporary directory")

	qaWatchArgs := []string{
		"-watch",
		"-format=tapj",
		"-listen-network", "tcp",
		"-listen-address", "127.0.0.1:0",
		"rspec:rspec/**/*spec.rb",
		// "minitest:minitest/**/test*.rb",
		// "test-unit:test-unit/**/test*.rb",
	}
	c := make(chan *testutil.Transcript, 10)
	quitCh := make(chan struct{})
	go qaWatch(quitCh, c, dir, qaWatchArgs...)
	defer close(quitCh)

	var source string
	var expect map[string]tapjio.Status

	// Write file into temp directory
	source = `RSpec.describe("Pass") { it("always passes") { expect(0).to eq 0 } }`
	expect = map[string]tapjio.Status{
		"always passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/simple_spec.rb", source, c, expect)

	// Write to nested directory
	// Wait for test result to be shown
	source = `RSpec.describe("Pass") { it("also passes") { expect(0).to eq 0 } }`
	expect = map[string]tapjio.Status{
		"also passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/simple2_spec.rb", source, c, expect)

	// Write test with bad require
	// Wait for error test result to be shown
	source = `require_relative 'rel_foobazbar'; RSpec.describe("Pass") { it("eventually passes") { expect(0).to eq 0 } }`
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/nested/overly_eager_spec.rb"): tapjio.Error,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/overly_eager_spec.rb", source, c, expect)

	// Write library that satisfies require_relative but does require
	// Expect test to be rerun
	source = `require 'lib_foobazbar'`
	writeFileAndAwaitResult(t, dir, "rspec/nested/rel_foobazbar.rb", source, c, expect)

	// Write library that satisfies require but does load
	// Expect test to be rerun
	source = `load File.expand_path("load_foobazbar.rb", File.dirname(__FILE__))`
	writeFileAndAwaitResult(t, dir, "lib/lib_foobazbar.rb", source, c, expect)

	// Write library that satisfies load
	// Expect test to be rerun
	source = ``
	expect = map[string]tapjio.Status{
		"eventually passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "lib/load_foobazbar.rb", source, c, expect)

	// Add another test that depends on library
	// Expect new test to be rerun
	source = `require_relative 'rel_foobazbar.rb'; RSpec.describe("Pass") { it("immediately passes") { expect(0).to eq 0 } }`
	expect = map[string]tapjio.Status{
		"immediately passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/new_spec.rb", source, c, expect)

	// Modify library
	source = `NotARealConstant`
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/nested/overly_eager_spec.rb"): tapjio.Error,
		filepath.Join(dir, "rspec/nested/new_spec.rb"):          tapjio.Error,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/rel_foobazbar.rb", source, c, expect)

	// Modify library
	source = `puts "hi"`
	expect = map[string]tapjio.Status{
		"eventually passes":  tapjio.Pass,
		"immediately passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/rel_foobazbar.rb", source, c, expect)

	removeFiles(t, dir, "rspec/nested/rel_foobazbar.rb")
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/nested/overly_eager_spec.rb"): tapjio.Error,
		filepath.Join(dir, "rspec/nested/new_spec.rb"):          tapjio.Error,
	}
	awaitResult(t, c, expect)

	source = `puts "hi"`
	expect = map[string]tapjio.Status{
		"eventually passes":  tapjio.Pass,
		"immediately passes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/rel_foobazbar.rb", source, c, expect)

	removeFiles(t, dir,
		"rspec/nested/overly_eager_spec.rb",
		"rspec/nested/rel_foobazbar.rb")
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/nested/new_spec.rb"): tapjio.Error,
	}
	awaitResult(t, c, expect)

	removeDir(t, dir, "rspec/nested")
	source = `RSpec.describe("Pass") { it("passes upon rising from the ashes") { expect(0).to eq 0 } }`
	expect = map[string]tapjio.Status{
		"passes upon rising from the ashes": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/nested/pheonix_spec.rb", source, c, expect)

	source = `require_relative '../../lib/lurking_lib.rb'; RSpec.describe("Pass") { it("passes upon its dep being renamed properly") { expect(0).to eq 0 } }`
	writeFile(t, dir, "rspec2/lurking/lurking_spec.rb", source)
	renameFile(t,
		filepath.Join(dir, "rspec2/lurking"),
		filepath.Join(dir, "rspec/lurking"))
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/lurking/lurking_spec.rb"): tapjio.Error,
	}
	awaitResult(t, c, expect)

	source = `puts "lurking"`
	writeFile(t, dir, "lib2/lurking_lib.rb", source)
	renameFile(t,
		filepath.Join(dir, "lib2/lurking_lib.rb"),
		filepath.Join(dir, "lib/lurking_lib.rb"))
	expect = map[string]tapjio.Status{
		"passes upon its dep being renamed properly": tapjio.Pass,
	}
	awaitResult(t, c, expect)

	renameFile(t,
		filepath.Join(dir, "lib/lurking_lib.rb"),
		filepath.Join(dir, "lib/lurking_lib2.rb"))
	expect = map[string]tapjio.Status{
		filepath.Join(dir, "rspec/lurking/lurking_spec.rb"): tapjio.Error,
	}
	awaitResult(t, c, expect)

	source = `
RSpec.describe("Pass") do
	it("passes upon its dep being renamed properly") do
		require_relative '../../lib/lurking_lib2.rb'
		expect(0).to eq 0
	end
end`
	expect = map[string]tapjio.Status{
		"passes upon its dep being renamed properly": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/lurking/lurking_spec.rb", source, c, expect)

	source = `puts "a"`
	writeFile(t, dir, "rspec/multi/a.rb", source)

	source = `puts "b1"`
	writeFile(t, dir, "rspec/multi/b1.rb", source)

	source = `puts "b2"`
	writeFile(t, dir, "rspec/multi/b2.rb", source)

	source = `
RSpec.describe("Pass") do
	it("tests a runtime require") do
		require_relative 'a'
		expect(0).to eq 0
	end

	it("tests another runtime require") do
		require_relative 'b1'
		expect(0).to eq 0
	end
end`
	expect = map[string]tapjio.Status{
		"tests a runtime require":       tapjio.Pass,
		"tests another runtime require": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/multi_spec.rb", source, c, expect)

	// Should really only run one test, but we don't have per test dependency support yet...
	source = `puts "b1."`
	expect = map[string]tapjio.Status{
		"tests another runtime require": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/b1.rb", source, c, expect)

	// Should really only run one test, but we don't have per test dependency support yet...
	source = `puts "a."`
	expect = map[string]tapjio.Status{
		"tests a runtime require": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/a.rb", source, c, expect)

	source = `
RSpec.describe("Pass") do
	it("tests a runtime require 2") do
		require_relative 'b1'
		expect(0).to eq 0
	end
end`
	expect = map[string]tapjio.Status{
		"tests a runtime require 2": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/control_spec.rb", source, c, expect)

	source = `
RSpec.describe("Pass") do
	it("tests a runtime require") do
		require_relative 'a'
		expect(0).to eq 0
	end

	it("tests a runtime require 3") do
		require_relative 'b2'
		expect(0).to eq 0
	end
end`
	expect = map[string]tapjio.Status{
		"tests a runtime require":   tapjio.Pass,
		"tests a runtime require 3": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/multi_spec.rb", source, c, expect)

	source = `puts "b1.."`
	expect = map[string]tapjio.Status{
		"tests a runtime require 2": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/b1.rb", source, c, expect)

	source = `puts "b2.."`
	expect = map[string]tapjio.Status{
		"tests a runtime require 3": tapjio.Pass,
	}
	writeFileAndAwaitResult(t, dir, "rspec/multi/b2.rb", source, c, expect)
}
