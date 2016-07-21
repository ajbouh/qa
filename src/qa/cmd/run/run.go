package run

// cd <basedir> && qa

import (
	// "bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-zglob"

	"qa/cmd"
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

const archiveNonceLexicon = "abcdefghijklmnopqrstuvwxyz"
const archiveNonceLength = 8
func randomString(r *rand.Rand, lexicon string, length int) string {
	bytes := make([]byte, length)
	lexiconLen := len(lexicon)
	for i := 0; i < length; i++ {
		bytes[i] = lexicon[r.Intn(lexiconLen)]
	}

	return string(bytes);
}

func Main(env *cmd.Env, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	archiveBaseDir := flags.String("archive-base-dir", "", "Base directory to store data for later analysis")
	auditDir := flags.String("audit-dir", "", "Directory to save any generated audits, e.g. TAP-J, JSON, SVG, etc.")
	quiet := flags.Bool("quiet", false, "Whether or not to print anything at all")
	saveTapj := flags.String("save-tapj", "", "Path to save TAP-J")
	saveTrace := flags.String("save-trace", "", "Path to save trace JSON")
	saveStacktraces := flags.String("save-stacktraces", "", "Path to save stacktraces.txt, implies -sample-stack")
	saveFlamegraph := flags.String("save-flamegraph", "", "Path to save flamegraph SVG, implies -sample-stack")
	saveIcegraph := flags.String("save-icegraph", "", "Path to save icegraph SVG, implies -sample-stack")
	savePalette := flags.String("save-palette", "palette.map", "Path to save (flame|ice)graph palette")
	format := flags.String("format", "pretty", "Set output format")
	jobs := flags.Int("jobs", runtime.NumCPU(), "Set number of jobs")

	showUpdatingSummary := flags.Bool("pretty-overwrite", true, "Pretty reporter shows live updating summary")
	elidePass := flags.Bool("pretty-quiet-pass", true, "Pretty reporter elides passing tests without (std)output")
	elideOmit := flags.Bool("pretty-quiet-omit", true, "Pretty reporter elides omitted tests without (std)output")

	errorsCaptureLocals := flags.String("errors-capture-locals", "false", "Use runtime debug API to capture locals from stack when raising errors")
	captureStandardFds := flags.Bool("capture-standard-fds", true, "Capture stdout and stderr")
	evalBeforeFork := flags.String("eval-before-fork", "", "Execute the given code before forking any workers or loading any files")
	evalAfterFork := flags.String("eval-after-fork", "", "Execute the given code after a work forks, but before work begins")
	sampleStack := flags.Bool("sample-stack", false, "Enable stack sampling")

	warmup := flags.Bool("warmup", true, "Try to warm up various worker caches")

	err := flags.Parse(args)
	if err != nil {
		return err
	}

	if *saveStacktraces != "" || *saveFlamegraph != "" || *saveIcegraph != "" {
		*sampleStack = true
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

	if !*quiet {
		switch *format {
		case "tapj":
			visitors = append(visitors, tapjio.NewTapjEmitter(env.Stdout))
		case "pretty":
			pretty := reporting.NewPretty(env.Stdout, *jobs)
			pretty.ShowUpdatingSummary = *showUpdatingSummary
			pretty.ElideQuietPass = *elidePass
			pretty.ElideQuietOmit = *elideOmit
			visitors = append(visitors, pretty)
		default:
			return errors.New(fmt.Sprintf("Unknown format: %v", *format))
		}
	}

	if *saveTapj != "" {
		tapjFile, err := os.Create(*saveTapj)
		if err != nil {
			return err
		}
		defer tapjFile.Close()
		visitors = append(visitors, tapjio.NewTapjEmitter(tapjFile))
	}

	if *archiveBaseDir != "" {
		now := time.Now()
		tapjArchiveDir := path.Join(*archiveBaseDir, now.Format("2006-01-02"))
		os.MkdirAll(tapjArchiveDir, 0755)
		r := rand.New(rand.NewSource(now.UnixNano()))
		nonce := randomString(r, archiveNonceLexicon, archiveNonceLength)

		tapjArchiveFilePath := path.Join(tapjArchiveDir, fmt.Sprintf("%d-%s.tapj", now.Unix(), nonce))
		tapjArchiveFile, err := os.Create(tapjArchiveFilePath)
		if err != nil {
			return err
		}
		defer tapjArchiveFile.Close()
		visitors = append(visitors, tapjio.NewTapjEmitter(tapjArchiveFile))
	}

	if *saveTrace != "" {
		traceFile, err := os.Create(*saveTrace)
		if err != nil {
			return err
		}
		defer traceFile.Close()
		visitors = append(visitors, tapjio.NewTraceWriter(traceFile))
	}

	var stacktracesFile *os.File
	if *saveStacktraces != "" {
		stacktracesFile, err = os.Create(*saveStacktraces)
		if err != nil {
			log.Fatal(err)
		}
		defer stacktracesFile.Close()
	}

	if *saveFlamegraph != "" || *saveIcegraph != "" {
		if stacktracesFile == nil {
			stacktracesFile, err = ioutil.TempFile("", "stacktrace")
			if err != nil {
				log.Fatal(err)
			}
			defer stacktracesFile.Close()
			defer os.Remove(stacktracesFile.Name())
		}
	}

	if stacktracesFile != nil {
		visitors = append(visitors, tapjio.NewStacktraceEmitter(stacktracesFile))
	}

	if *saveFlamegraph != "" {
		visitors = append(visitors, &tapjio.DecodingCallbacks{
			OnFinal: func(final tapjio.FinalEvent) error {
				titleSuffix, _ := json.Marshal(flags.Args())
				options := []string{
					"--title", "Flame Graph — jobs = " + strconv.Itoa(*jobs) + ", args = " + string(titleSuffix),
					"--minwidth=2",
				}
				if *savePalette != "" {
					options = append(options, "--cp", "--palfile="+*savePalette)
				}

				// There may be nothing to do if we didn't see any stacktrace data!
				stacktraceFileInfo, err := stacktracesFile.Stat()
				if err != nil {
					return err
				}
				if stacktraceFileInfo.Size() == 0 {
					return nil
				}

				flamegraphFile, err := os.Create(*saveFlamegraph)
				if err != nil {
					log.Fatal(err)
				}
				defer flamegraphFile.Close()

				stacktracesFile.Seek(0, 0)
				err = tapjio.GenerateFlameGraph(
					stacktracesFile,
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
					options = append(options, "--cp", "--palfile="+*savePalette)
				}

				// There may be nothing to do if we didn't see any stacktrace data!
				stacktraceFileInfo, err := stacktracesFile.Stat()
				if err != nil {
					return err
				}
				if stacktraceFileInfo.Size() == 0 {
					return nil
				}

				icegraphFile, err := os.Create(*saveIcegraph)
				if err != nil {
					log.Fatal(err)
				}
				defer icegraphFile.Close()

				stacktracesFile.Seek(0, 0)
				err = tapjio.GenerateFlameGraph(
					stacktracesFile,
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
		return err
	}

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for signal
		sig := <-c
		fmt.Fprintln(env.Stderr, "Got signal:", sig)
		srv.Close()
		os.Exit(1)
	}(sigc)

	defer srv.Close()
	go srv.Run()

	workerEnvs := []map[string]string{}
	for i := 0; i < *jobs; i++ {
		workerEnvs = append(workerEnvs,
			map[string]string{"QA_WORKER": fmt.Sprintf("%d", i)})
	}

	seed := int(rand.Int31())

	// TODO(adamb) Parallelize this, after sanitizing name/globs specs.
	var allRunners []runner.TestRunner
	for _, runnerSpec := range flags.Args() {
		runnerSpecSplit := strings.SplitN(runnerSpec, ":", 2)
		var runnerName string
		var globStr string
		if len(runnerSpecSplit) != 2 {
			runnerName = runnerSpecSplit[0]
			globStr = emitter.DefaultGlob(runnerName)
		} else {
			runnerName = runnerSpecSplit[0]
			globStr = runnerSpecSplit[1]
		}

		var files []string

		for _, glob := range strings.Split(globStr, ":") {
			relative := !filepath.IsAbs(glob)
			if relative && env.Dir != "" {
				glob = filepath.Join(env.Dir, glob)
			}

			globFiles, err := zglob.Glob(glob)
			if !*quiet {
				fmt.Fprintf(env.Stderr, "Resolved %v to %v\n", glob, globFiles)
				if err != nil {
					return err
				}
			}

			if relative && env.Dir != "" {
				trimPrefix := fmt.Sprintf("%s%c", env.Dir, os.PathSeparator)
				for _, file := range globFiles {
					files = append(files, strings.TrimPrefix(file, trimPrefix))
				}
			} else {
				files = append(files, globFiles...)
			}
		}

		passthrough := map[string](interface{}){
			"warmup":              *warmup,
			"errorsCaptureLocals": *errorsCaptureLocals,
			"captureStandardFds":  *captureStandardFds,
			"evalBeforeFork":      *evalBeforeFork,
			"evalAfterFork":       *evalAfterFork,
			"sampleStack":         *sampleStack,
		}

		em, err := emitter.Resolve(runnerName, srv, passthrough, workerEnvs, env.Dir, env.Vars, seed, files)
		if err != nil {
			return err
		}

		traceEvents, runners, err := em.EnumerateTests()
		if err != nil {
			return err
		}

		allRunners = append(allRunners, runners...)

		for _, traceEvent := range traceEvents {
			err := visitor.TraceEvent(traceEvent)
			if err != nil {
				return err
			}
		}
	}

	suite := suite.NewTestSuiteRunner(seed, srv, allRunners)

	var final tapjio.FinalEvent

	final, err = suite.Run(workerEnvs, visitor)
	if err != nil {
		fmt.Fprintln(env.Stderr, "Error in NewTestSuiteRunner", err)
		if exitError, ok := err.(*exec.ExitError); ok {
			if len(exitError.Stderr) > 0 {
				fmt.Fprintln(env.Stderr, string(exitError.Stderr))
			}
		}

		return err
	}

	if !final.Passed() {
		return errors.New("Test(s) failed.")
	}

	return nil
}
