package run

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path"
	"time"

	"qa/cmd"
	"qa/reporting"
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

	return string(bytes)
}

func newArchiveTapjEmitter(archiveBaseDir string) (tapjio.Visitor, error) {
	now := time.Now()
	tapjArchiveDir := path.Join(archiveBaseDir, now.Format("2006-01-02"))
	os.MkdirAll(tapjArchiveDir, 0755)
	r := rand.New(rand.NewSource(now.UnixNano()))
	nonce := randomString(r, archiveNonceLexicon, archiveNonceLength)

	tapjArchiveFilePath := path.Join(tapjArchiveDir, fmt.Sprintf("%d-%s.tapj", now.Unix(), nonce))
	tapjArchiveFile, err := os.Create(tapjArchiveFilePath)
	if err != nil {
		return nil, err
	}

	return tapjio.NewTapjEmitCloser(tapjArchiveFile), nil
}

type outputFlags struct {
	archiveBaseDir      *string
	auditDir            *string
	quiet               *bool
	saveTapj            *string
	saveTrace           *string
	saveStacktraces     *string
	saveFlamegraph      *string
	saveIcegraph        *string
	savePalette         *string
	format              *string
	showUpdatingSummary *bool
	showIndividualTests *bool
	elidePass           *bool
	elideOmit           *bool
	showSnails          *bool
}

func defineOutputFlags(flags *flag.FlagSet) *outputFlags {
	return &outputFlags{
		archiveBaseDir:      flags.String("archive-base-dir", "", "Base directory to store data for later analysis"),
		auditDir:            flags.String("audit-dir", "", "Directory to save any generated audits, e.g. TAP-J, JSON, SVG, etc."),
		quiet:               flags.Bool("quiet", false, "Whether or not to print anything at all"),
		saveTapj:            flags.String("save-tapj", "", "Path to save TAP-J"),
		saveTrace:           flags.String("save-trace", "", "Path to save trace JSON"),
		saveStacktraces:     flags.String("save-stacktraces", "", "Path to save stacktraces.txt, implies -sample-stack"),
		saveFlamegraph:      flags.String("save-flamegraph", "", "Path to save flamegraph SVG, implies -sample-stack"),
		saveIcegraph:        flags.String("save-icegraph", "", "Path to save icegraph SVG, implies -sample-stack"),
		savePalette:         flags.String("save-palette", "palette.map", "Path to save (flame|ice)graph palette"),
		format:              flags.String("format", "pretty", "Set output format"),
		showUpdatingSummary: flags.Bool("pretty-overwrite", true, "Pretty reporter shows live updating summary. Forces -pretty-quite-pass=false, -pretty-quiet-omit=false"),
		showIndividualTests: flags.Bool("pretty-show-individual-tests", true, "Pretty reporter shows output for individual tests"),
		elidePass:           flags.Bool("pretty-quiet-pass", true, "Pretty reporter elides passing tests without (std)output"),
		elideOmit:           flags.Bool("pretty-quiet-omit", true, "Pretty reporter elides omitted tests without (std)output"),
		showSnails:          flags.Bool("pretty-show-snails", true, "Pretty reporter shows tests dramatically slower than others"),
	}
}

func (f *outputFlags) newVisitor(env *cmd.Env, jobs int, svgTitleSuffix string) (tapjio.Visitor, error) {
	saveTapj := *f.saveTapj
	saveTrace := *f.saveTrace
	saveStacktraces := *f.saveStacktraces
	saveFlamegraph := *f.saveFlamegraph
	saveIcegraph := *f.saveIcegraph
	savePalette := *f.savePalette

	auditDir := *f.auditDir

	if auditDir != "" {
		auditDir = maybeJoin(auditDir, env.Dir)
		os.MkdirAll(auditDir, 0755)

		saveTapj = maybeJoin(saveTapj, auditDir)
		saveTrace = maybeJoin(saveTrace, auditDir)
		saveStacktraces = maybeJoin(saveStacktraces, auditDir)
		saveFlamegraph = maybeJoin(saveFlamegraph, auditDir)
		saveIcegraph = maybeJoin(saveIcegraph, auditDir)
		savePalette = maybeJoin(savePalette, auditDir)
	}

	var visitors []tapjio.Visitor
	var err error

	if !*f.quiet {
		switch *f.format {
		case "tapj":
			visitors = append(visitors, tapjio.NewTapjEmitter(env.Stdout))
		case "pretty":
			pretty := reporting.NewPretty(env.Stdout, jobs)
			pretty.ShowIndividualTests = *f.showIndividualTests
			if pretty.ShowIndividualTests {
				pretty.ShowSnails = *f.showSnails
			} else {
				pretty.ShowSnails = false
			}
			pretty.ShowUpdatingSummary = *f.showUpdatingSummary
			if pretty.ShowUpdatingSummary {
				pretty.ElideQuietPass = *f.elidePass
				pretty.ElideQuietOmit = *f.elideOmit
			} else {
				pretty.ElideQuietPass = false
				pretty.ElideQuietOmit = false
			}
			visitors = append(visitors, pretty)
		default:
			return nil, errors.New(fmt.Sprintf("Unknown format: %v", *f.format))
		}
	}

	if saveTapj != "" {
		tapjFile, err := os.Create(saveTapj)
		if err != nil {
			return nil, err
		}
		visitors = append(visitors, tapjio.NewTapjEmitCloser(tapjFile))
	}

	archiveBaseDir := *f.archiveBaseDir
	if archiveBaseDir != "" {
		archiveBaseDir = maybeJoin(archiveBaseDir, env.Dir)
		visitor, err := newArchiveTapjEmitter(archiveBaseDir)
		if err != nil {
			return nil, err
		}
		visitors = append(visitors, visitor)
	}

	if saveTrace != "" {
		traceFile, err := os.Create(saveTrace)
		if err != nil {
			return nil, err
		}
		visitors = append(visitors, tapjio.NewTraceWriter(traceFile))
	}

	var stacktracesFile *os.File
	if saveStacktraces != "" {
		stacktracesFile, err = os.Create(saveStacktraces)
		if err != nil {
			log.Fatal(err)
		}
	}

	removeStacktracesFileAfterUse := false
	if saveFlamegraph != "" || saveIcegraph != "" {
		if stacktracesFile == nil {
			stacktracesFile, err = ioutil.TempFile("", "stacktrace")
			if err != nil {
				log.Fatal(err)
			}
			removeStacktracesFileAfterUse = true
		}
	}

	if stacktracesFile != nil {
		visitors = append(visitors, tapjio.NewStacktraceEmitCloser(stacktracesFile))
	}

	if saveFlamegraph != "" {
		visitors = append(visitors, &tapjio.DecodingCallbacks{
			OnSuiteFinish: func(final tapjio.SuiteFinishEvent) error {
				options := []string{
					"--title", "Flame Graph" + svgTitleSuffix,
					"--minwidth=2",
				}
				if savePalette != "" {
					options = append(options, "--cp", "--palfile="+savePalette)
				}

				// There may be nothing to do if we didn't see any stacktrace data!
				stacktraceFileInfo, err := stacktracesFile.Stat()
				if err != nil {
					return err
				}
				if stacktraceFileInfo.Size() == 0 {
					return nil
				}

				flamegraphFile, err := os.Create(saveFlamegraph)
				if err != nil {
					log.Fatal(err)
				}

				stacktracesFile.Seek(0, 0)
				defer stacktracesFile.Close()
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

	if saveIcegraph != "" {
		visitors = append(visitors, &tapjio.DecodingCallbacks{
			OnSuiteFinish: func(final tapjio.SuiteFinishEvent) error {
				options := []string{
					"--title", "Icicle Graph" + svgTitleSuffix,
					"--minwidth=2",
					"--reverse",
					"--inverted",
				}
				if savePalette != "" {
					options = append(options, "--cp", "--palfile="+savePalette)
				}

				// There may be nothing to do if we didn't see any stacktrace data!
				stacktraceFileInfo, err := stacktracesFile.Stat()
				if err != nil {
					return err
				}
				if stacktraceFileInfo.Size() == 0 {
					return nil
				}

				icegraphFile, err := os.Create(saveIcegraph)
				if err != nil {
					log.Fatal(err)
				}

				stacktracesFile.Seek(0, 0)
				defer stacktracesFile.Close()
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

	if removeStacktracesFileAfterUse {
		visitors = append(visitors,
			&tapjio.DecodingCallbacks{
				OnEnd: func(err error) error { return os.Remove(stacktracesFile.Name()) },
			})
	}

	return tapjio.MultiVisitor(visitors), nil
}
