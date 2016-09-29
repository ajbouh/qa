package flaky

import (
  "bytes"
	"io"
  "encoding/json"
  "qa/cmd"
  "qa/cmd/discover"
  "qa/cmd/grouping"
  "qa/cmd/summary"
  "qa/pipeline"
  "strconv"
)

const summaryCollapseId = "suite.label,case-labels,label"
const filterCollapseId = "suite.coderef," + summaryCollapseId

type Session struct {
	ArchiveBaseDir string
	NumDays        int
	UntilDate      string
	Stderr         io.Writer

	ProgramName    string
	summaries      []*TestSummary
}

func (s *Session) Summaries() ([]*TestSummary, error) {
	if s.summaries != nil {
		return s.summaries, nil
	}

	stdoutBuf := &bytes.Buffer{}

	err := pipeline.Run(
		&cmd.Env{Stdin: bytes.NewBuffer([]byte{}), Stdout: stdoutBuf, Stderr: s.Stderr},
		[]pipeline.Op{
			pipeline.Op{
				Main: discover.Main,
				Argv: []string{
					"qa discover",
					"--dir", s.ArchiveBaseDir,
					"--number-days", strconv.Itoa(s.NumDays),
					"--until-date", s.UntilDate,
				},
			},
			pipeline.Op{
				Main: grouping.Main,
				Argv: []string{
					"qa group",
					"--collapse-id", filterCollapseId,
					"--keep-if-any", "status==\"pass\"",
					"--keep-residual-records-matching-kept", "outcome-digest",
				},
			},
			pipeline.Op{
				Main: summary.Main,
				Argv: []string{
					"qa summary",
					"--duration", "time",
					"--no-show-aces",
					"--sort-by", "suite.start",
					"--group-by", summaryCollapseId,
					"--subgroup-by", "outcome-digest",
					"--ignore-if", "status==\"todo\"",
					"--ignore-if", "status==\"omit\"",
					"--success-if", "status==\"pass\"",
				},
			},
		},
	)

	decoder := json.NewDecoder(bytes.NewBuffer(stdoutBuf.Bytes()))
	summaries := []*TestSummary{}
	for {
		if summary, err := DecodeSummary(decoder); err == nil {
			summaries = append(summaries, summary)
		} else if err == io.EOF {
			err = nil
			break
		} else {
			return nil, err
		}
	}

	if err != nil {
		s.summaries = summaries
	}

	return summaries, err
}
