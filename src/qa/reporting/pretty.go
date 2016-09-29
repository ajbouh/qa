package reporting

import (
	"fmt"
	"io"
	"qa/analysis"
	"qa/tapjio"
	"strings"
	"time"
)

type Pretty struct {
	ElideQuietPass      bool
	ElideQuietOmit      bool
	ShowUpdatingSummary bool
	ShowSnails          bool
	ShowIndividualTests bool

	writer        io.Writer
	varyingSeeds  bool
	runs          int
	run           int
	jobs          int
	lastCase      *tapjio.CaseEvent
	startTime     time.Time
	totalTests    int
	totalTestTime float64
	seed          int
	timeCop       *analysis.TimeCop
	tally         *tapjio.ResultTally

	mostRecentTestPrintedSpacingNewline bool

	pending map[tapjio.TestFilter]string

	style *Style
}

func NewPretty(writer io.Writer, jobs int, runs int, varyingSeeds bool) *Pretty {
	return &Pretty{
		writer:       writer,
		jobs:         jobs,
		run:          0,
		runs:         runs,
		varyingSeeds: varyingSeeds,
		style:        NewStyle(),
	}
}

func (self *Pretty) TraceEvent(trace tapjio.TraceEvent) error {
	return nil
}

func (self *Pretty) AwaitAttach(event tapjio.AwaitAttachEvent) error {
	return nil
}

func (self *Pretty) SuiteBegin(suite tapjio.SuiteBeginEvent) error {
	self.pending = make(map[tapjio.TestFilter]string)
	self.timeCop = &analysis.TimeCop{MaxResults: 10}

	self.run += 1
	self.seed = suite.Seed

	if self.run == 1 {
		self.tally = &tapjio.ResultTally{}
		self.totalTestTime = 0
		self.totalTests = suite.Count * self.runs
		self.startTime = time.Now()

		omissions := []string{}
		if self.ElideQuietPass {
			omissions = append(omissions, "passing")
		}

		if self.ElideQuietOmit {
			omissions = append(omissions, "omitted")
		}

		omissionsDesc := ""
		if len(omissions) > 0 {
			omissionsDesc = fmt.Sprintf(" Will only show %s tests with output.",
				strings.Join(omissions, "/"))
		}

		maybeRuns := ""
		if self.runs > 1 {
			maybeRuns = fmt.Sprintf(" %d times", self.runs)
		}

		var seed string
		if self.varyingSeeds && self.runs > 1 {
			seed = "varying seeds"
		} else {
			seed = fmt.Sprintf("seed %d", self.seed)
		}

		fmt.Fprintf(self.writer, "Will run %d %s%s using %d %s and %s.%s..\n",
			suite.Count,
			MaybePlural(suite.Count, "test", "tests"),
			maybeRuns,
			self.jobs,
			MaybePlural(self.jobs, "job", "jobs"),
			seed,
			omissionsDesc)
	}

	self.writeSummary()
	return nil
}

func (self *Pretty) clearSummary() {
	if !self.ShowUpdatingSummary {
		return
	}

	if self.ShowIndividualTests {
		n := 1 + len(self.pending)
		for i := 0; i < n; i++ {
			// Clear line, beginning of line, move up, clear line
			fmt.Fprintf(self.writer, "\033[2K\r\033[1A\033[2K")
		}
	} else {
		// Clear line, beginning of line
		fmt.Fprintf(self.writer, "\033[2K\r")
	}
}

func (self *Pretty) writeSummary() {
	if !self.ShowUpdatingSummary {
		return
	}

	numPending := len(self.pending)

	tallySummaryPrefix := ""
	tallySummary := self.style.FormatTally(*self.tally)
	if tallySummary != "" {
		tallySummaryPrefix = ": "
		tallySummary = tallySummary + ", with"
	}

	lineSuffix := ""
	if self.ShowIndividualTests {
		lineSuffix = ":\n"
	}

	fmt.Fprintf(self.writer, "Ran %d%% in %v (%v of job time)%s%s %d remaining and %d running%s",
		int(float64(self.tally.Total)/float64(self.totalTests)*100.0),
		Round(time.Since(self.startTime), time.Millisecond),
		millisDuration(self.totalTestTime),
		tallySummaryPrefix,
		tallySummary,
		self.totalTests-self.tally.Total-numPending,
		numPending,
		lineSuffix)

	if self.ShowIndividualTests {
		for _, label := range self.pending {
			fmt.Fprintf(self.writer, "  %s\n", self.style.FormatTestDescription(label))
		}
	}
}

func (self *Pretty) TestBegin(event tapjio.TestBeginEvent) error {
	self.clearSummary()
	self.pending[event.Filter] = tapjio.TestLabel(event.Label, event.Cases)
	self.writeSummary()

	return nil
}

func (self *Pretty) TestFinish(test tapjio.TestFinishEvent) error {
	self.timeCop.TestFinish(test)
	self.tally.Increment(test.Status)

	self.totalTestTime += test.Time

	self.clearSummary()

	delete(self.pending, test.Filter)
	defer self.writeSummary()

	if !self.ShowIndividualTests {
		return nil
	}

	if ((self.ElideQuietPass && test.Status == tapjio.Pass) ||
		(self.ElideQuietOmit && test.Status == tapjio.Omit)) &&
		test.Stdout == "" && test.Stderr == "" {
		return nil
	}

	self.style.SummarizeTestFinish(self.writer, self.totalTests, *self.tally, test)

	if test.Stdout != "" || test.Stderr != "" {
		self.mostRecentTestPrintedSpacingNewline = true
		fmt.Fprintf(self.writer, "\n")
	} else {
		self.mostRecentTestPrintedSpacingNewline = false
	}

	return nil
}

func (self *Pretty) SuiteFinish(final tapjio.SuiteFinishEvent) error {
	self.timeCop.SuiteFinish(final)
	self.clearSummary()

	counts := final.Counts

	// If there are errors/fails don't show any SLOW PASSes
	if self.ShowSnails {
		if self.timeCop.Passed() && len(self.timeCop.SlowPassingOutcomes) > 0 {
			if !self.mostRecentTestPrintedSpacingNewline {
				fmt.Fprintf(self.writer, "\n")
			}

			self.style.SummarizeSnails(self.writer, self.timeCop)
		}
	}

	if self.runs == self.run {
		fmt.Fprintf(self.writer, "🏁  Ran %d tests in %v (%v of job time): %s.\n",
			counts.Total,
			millisDuration(final.Time),
			millisDuration(self.timeCop.TotalDuration),
			self.style.FormatTally(*counts))
	}

	return nil
}

func (t *Pretty) End(reason error) error {
	return nil
}
