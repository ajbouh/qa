package run

// cd <basedir> && qa

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/mattn/go-zglob"

	"qa/emitter"
	"qa/reporting"
	"qa/runner"
	"qa/runner/server"
	"qa/suite"
	"qa/tapjio"
)

func maybeJoin(p string, dir string) string {
	if p != "" && p[0] != '.' && p[0] != '/' {
		return path.Join(dir, p)
	}

	return p
}

func Main(args []string) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	auditDir := flags.String("audit-dir", "", "Directory to save TAP-J, JSON, SVG")
	saveTapj := flags.String("save-tapj", "results.tapj", "Path to save TAP-J")
	saveTrace := flags.String("save-trace", "trace.json", "Path to save trace JSON")
	saveStacktraces := flags.String("save-stacktraces", "stacktraces.txt", "Path to save stacktraces.txt")
	saveFlamegraph := flags.String("save-flamegraph", "flamegraph.svg", "Path to save flamegraph SVG")
	saveIcegraph := flags.String("save-icegraph", "icegraph.svg", "Path to save icegraph SVG")
	savePalette := flags.String("save-palette", "palette.map", "Path to save (flame|ice)graph palette")
	format := flags.String("format", "pretty", "Set output format")
	jobs := flags.Int("jobs", runtime.NumCPU(), "Set number of jobs")

	err := flags.Parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if *auditDir != "" {
		os.MkdirAll(*auditDir, 0755)

		*saveTapj = maybeJoin(*saveTapj, *auditDir)
		*saveTrace = maybeJoin(*saveTrace, *auditDir)
		*saveStacktraces = maybeJoin(*saveStacktraces, *auditDir)
		*saveFlamegraph = maybeJoin(*saveFlamegraph, *auditDir)
		*saveIcegraph = maybeJoin(*saveIcegraph, *auditDir)
		*savePalette = maybeJoin(*savePalette, *auditDir)
	}

	var visitors []tapjio.Visitor

	switch *format {
	case "tapj":
		visitors = append(visitors, tapjio.NewTapjEmitter(os.Stdout))
	case "pretty":
		visitors = append(visitors, &reporting.Pretty{Writer: os.Stdout, Jobs: *jobs})
	default:
		fmt.Fprintln(os.Stderr, "Unknown format", *format)
		return 254
	}

	if *saveTapj != "" {
		tapjFile, err := os.Create(*saveTapj)
		if err != nil {
			log.Fatal(err)
		}
		defer tapjFile.Close()
		visitors = append(visitors, tapjio.NewTapjEmitter(tapjFile))
	}

	if *saveTrace != "" {
		traceFile, err := os.Create(*saveTrace)
		if err != nil {
			log.Fatal(err)
		}
		defer traceFile.Close()
		visitors = append(visitors, tapjio.NewTraceWriter(traceFile))
	}

	stacktraceWriters := []io.Writer{}
	if *saveStacktraces != "" {
		stacktracesFile, err := os.Create(*saveStacktraces)
		if err != nil {
			log.Fatal(err)
		}
		defer stacktracesFile.Close()
		stacktraceWriters = append(stacktraceWriters, stacktracesFile)
	}

	var stacktraceBytes bytes.Buffer
	if *saveFlamegraph != "" || *saveIcegraph != "" {
		stacktraceWriters = append(stacktraceWriters, &stacktraceBytes)
	}

	if len(stacktraceWriters) > 0 {
		visitors = append(visitors, tapjio.NewStacktraceEmitter(io.MultiWriter(stacktraceWriters...)))
	}

	if *saveFlamegraph != "" {
		flamegraphFile, err := os.Create(*saveFlamegraph)
		if err != nil {
			log.Fatal(err)
		}
		defer flamegraphFile.Close()

		visitors = append(visitors, &tapjio.DecodingCallbacks{
			OnFinal: func(final tapjio.FinalEvent) error {
				titleSuffix, _ := json.Marshal(flags.Args())
				options := []string{
					"--title", "Flame Graph — jobs = " + strconv.Itoa(*jobs) + ", args = " + string(titleSuffix),
					"--minwidth=2",
				}
				if *savePalette != "" {
					options = append(options, "--cp", "--palfile=" + *savePalette)
				}

				err = tapjio.GenerateFlameGraph(
					bytes.NewReader(stacktraceBytes.Bytes()),
					flamegraphFile,
					options...)
				if err != nil {
					return err
				}
				return nil
			},
		})
	}

	if *saveIcegraph != "" {
		icegraphFile, err := os.Create(*saveIcegraph)
		if err != nil {
			log.Fatal(err)
		}
		defer icegraphFile.Close()

		visitors = append(visitors, &tapjio.DecodingCallbacks{
			OnFinal: func(final tapjio.FinalEvent) error {
				titleSuffix, _ := json.Marshal(flags.Args())
				options := []string{
					"--title", "Icicle Graph — jobs = " + strconv.Itoa(*jobs) + ", args = " + string(titleSuffix),
					"--minwidth=2",
					"--reverse",
					"--inverted",
				}
				if *savePalette != "" {
					options = append(options, "--cp", "--palfile=" + *savePalette)
				}

				err = tapjio.GenerateFlameGraph(
					bytes.NewReader(stacktraceBytes.Bytes()),
					icegraphFile,
					options...)
				if err != nil {
					return err
				}
				return nil
			},
		})
	}

	visitor := tapjio.MultiVisitor(visitors)

	// srv, err := server.Listen("tcp", "127.0.0.1:0")
	srv, err := server.Listen("unix", "/tmp/qa")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Starting internal server failed.", err)
		return 1
	}

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for signal
		sig := <-c
		fmt.Fprintln(os.Stderr, "Got signal: ", sig)
		srv.Close()
		os.Exit(1)
	}(sigc)

	defer srv.Close()
	go srv.Run()

	seed := int(rand.Int31())

	// TODO(adamb) Parallelize this, after sanitizing name/globs specs.
	var allRunners []runner.TestRunner
	for _, runnerSpec := range flags.Args() {
		runnerSpecSplit := strings.SplitN(runnerSpec, ":", 2)
		var runnerName string
		var glob string
		if len(runnerSpecSplit) != 2 {
			// TODO(adamb) Should autodetect. For now assume rspec.
			runnerName = "rspec"
			glob = runnerSpecSplit[0]
		} else {
			runnerName = runnerSpecSplit[0]
			glob = runnerSpecSplit[1]
		}

		files, err := zglob.Glob(glob)
		fmt.Fprintf(os.Stderr, "Resolved %v to %v\n", glob, files)

		if err != nil {
			fmt.Fprintln(os.Stderr, "Resolving glob.", err)
			return 1
		}

		em, err := emitter.Resolve(runnerName, srv, seed, files)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Starting rspec context.", err)
			return 1
		}

		traceEvents, runners, err := em.EnumerateTests(seed)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Enumerating tests failed.", err)
			return 1
		}

		allRunners = append(allRunners, runners...)

		for _, traceEvent := range traceEvents {
			err := visitor.TraceEvent(traceEvent)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Trace event processing failed", err)
				return 1
			}
		}
	}

	fmt.Fprintln(os.Stderr, "Creating NewTestSuiteRunner")
	suite := suite.NewTestSuiteRunner(seed, srv, allRunners)

	var final tapjio.FinalEvent
	fmt.Fprintln(os.Stderr, "Running NewTestSuiteRunner")

	final, err = suite.Run(*jobs, visitor)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error in NewTestSuiteRunner", err)
		if exitError, ok := err.(*exec.ExitError); ok {
			if len(exitError.Stderr) > 0 {
				fmt.Fprintln(os.Stderr, string(exitError.Stderr))
			}

			waitStatus := exitError.Sys().(syscall.WaitStatus)
			return waitStatus.ExitStatus()
		}

		fmt.Fprintln(os.Stderr, "Test runner enumeration failed.")
		return 1
	}

	if !final.Passed() {
		return 1
	}

	return 0
}
