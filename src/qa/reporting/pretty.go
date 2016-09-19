package reporting

import (
	"fmt"
	"io"
	"qa/analysis"
	"qa/tapjio"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

type Pretty struct {
	ElideQuietPass      bool
	ElideQuietOmit      bool
	ShowUpdatingSummary bool
	ShowSnails          bool
	ShowIndividualTests bool

	writer        io.Writer
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

	boldYellow  func(a ...interface{}) string
	yellow      func(a ...interface{}) string
	cyan        func(a ...interface{}) string
	green       func(a ...interface{}) string
	red         func(a ...interface{}) string
	magenta     func(a ...interface{}) string
	boldMagenta func(a ...interface{}) string
	bold        func(a ...interface{}) string
}

func NewPretty(writer io.Writer, jobs int) *Pretty {
	return &Pretty{
		writer:      writer,
		jobs:        jobs,
		boldYellow:  color.New(color.Bold, color.FgYellow).SprintFunc(),
		yellow:      color.New(color.FgYellow).SprintFunc(),
		cyan:        color.New(color.FgCyan).SprintFunc(),
		green:       color.New(color.FgGreen).SprintFunc(),
		red:         color.New(color.FgRed).SprintFunc(),
		magenta:     color.New(color.FgMagenta).SprintFunc(),
		boldMagenta: color.New(color.Bold, color.FgMagenta).SprintFunc(),
		bold:        color.New(color.Bold).SprintFunc(),
	}
}

func (self *Pretty) summarizeTally(tally tapjio.ResultTally) string {
	countLabels := []string{}
	if tally.Pass > 0 {
		countLabels = append(countLabels,
			self.green(tally.Pass, maybePlural(tally.Pass, " pass", " passes")))
	}

	if tally.Fail > 0 {
		countLabels = append(countLabels,
			self.red(tally.Fail, maybePlural(tally.Fail, " fail", " fails")))
	}

	if tally.Error > 0 {
		countLabels = append(countLabels,
			self.magenta(tally.Error, maybePlural(tally.Error, " error", " errors")))
	}

	if tally.Todo > 0 {
		countLabels = append(countLabels,
			self.cyan(tally.Todo, maybePlural(tally.Todo, " skip", " skips")))
	}

	if tally.Omit > 0 {
		countLabels = append(countLabels,
			self.cyan(tally.Omit, maybePlural(tally.Omit, " omit", " omits")))
	}

	return strings.Join(countLabels, ", ")
}

func (self *Pretty) TraceEvent(trace tapjio.TraceEvent) error {
	return nil
}

func (self *Pretty) SuiteBegin(suite tapjio.SuiteBeginEvent) error {
	self.pending = make(map[tapjio.TestFilter]string)
	self.timeCop = &analysis.TimeCop{MaxResults: 10}
	self.tally = &tapjio.ResultTally{}

	self.totalTestTime = 0
	self.totalTests = suite.Count
	self.seed = suite.Seed
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

	fmt.Fprintf(self.writer, "Will run %d tests using %d %s and seed %d.%s..\n",
		self.totalTests,
		self.jobs,
		maybePlural(self.jobs, "job", "jobs"),
		self.seed,
		omissionsDesc)

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
	tallySummary := self.summarizeTally(*self.tally)
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
		round(time.Since(self.startTime), time.Millisecond),
		millisDuration(self.totalTestTime),
		tallySummaryPrefix,
		tallySummary,
		self.totalTests-self.tally.Total-numPending,
		numPending,
		lineSuffix)

	if self.ShowIndividualTests {
		for _, label := range self.pending {
			fmt.Fprintf(self.writer, "  %s\n", self.bold(label))
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

	label := tapjio.TestLabel(test.Label, test.Cases)
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

	var status string
	switch test.Status {
	case tapjio.Pass:
		status = self.green("✔") // " PASS"
	case tapjio.Fail:
		status = self.red("✖") // " FAIL"
	case tapjio.Error:
		status = self.boldMagenta("!") // "ERROR"
	case tapjio.Omit:
		status = self.cyan("Ø") // " OMIT"
	case tapjio.Todo:
		status = self.cyan("…") // " SKIP"
	default:
		status = self.red("?")
	}

	description := fmt.Sprintf("%s", self.bold(label))

	fmt.Fprintf(self.writer, "%s  %-50s [%d/%d] %v\n",
		status,
		description,
		self.tally.Total, self.totalTests,
		millisDuration(test.Time))

	if (test.Status == tapjio.Fail || test.Status == tapjio.Error) && test.Exception != nil {
		// fmt.Fprintf(self.writer, "%s\n\n", indent(test.Exception.Class, 3))
		fmt.Fprintf(self.writer, "%s\n\n", indent(test.Exception.Message, 3))

		snippets := test.Exception.Snippets
		for _, entry := range test.Exception.Backtrace {
			fmt.Fprintf(self.writer, "   File \"%s\", line %d\n", entry.File, entry.Line)
			vars := entry.Variables
			if len(vars) > 0 {
				fmt.Fprintf(self.writer, "   Locals:\n")

				maxVarNameLength := 0
				for varName, _ := range vars {
					l := len(varName)
					if l > maxVarNameLength {
						maxVarNameLength = l
					}
				}

				format := "      %- " + strconv.Itoa(maxVarNameLength) + "s = %s\n"
				for varName, varValue := range vars {
					singleLineValue := strings.Replace(varValue, "\n", "↩ ", -1)
					fmt.Fprintf(self.writer, format, varName, singleLineValue)
				}
			}

			lines, ok := snippets[entry.File]
			if !ok {
				continue
			}

			fmt.Fprintf(self.writer, "   Source:\n")
			contextLines := 3
			initialLine := entry.Line - contextLines
			lastLine := entry.Line + contextLines
			if initialLine < 0 {
				initialLine = 1
			}

			for i := initialLine; i <= lastLine; i++ {
				lineText, ok := lines[strconv.Itoa(i)]
				if !ok {
					continue
				}
				var marker string
				format := "   %s% 3d | %s\n"
				if i == entry.Line {
					marker = "›"
					format = self.boldYellow(format)
				} else {
					marker = " "
				}
				fmt.Fprintf(self.writer, format, marker, i, lineText)
			}

		}

		fmt.Fprintf(self.writer, "\n")
	}

	if test.Stdout != "" {
		endl := "\n"
		if strings.HasSuffix(test.Stdout, "\n") {
			endl = ""
		}
		fmt.Fprintf(self.writer, "   %s\n%s%s", "STDOUT", indent(test.Stdout, 3), endl)
	}

	if test.Stderr != "" {
		endl := "\n"
		if strings.HasSuffix(test.Stderr, "\n") {
			endl = ""
		}
		fmt.Fprintf(self.writer, "   %s\n%s%s", "STDERR", indent(test.Stderr, 3), endl)
	}

	if test.Stdout != "" || test.Stderr != "" {
		self.mostRecentTestPrintedSpacingNewline = true
		fmt.Fprintf(self.writer, "\n")
	} else {
		self.mostRecentTestPrintedSpacingNewline = false
	}

	return nil
}

func parseBacktracePosition(backtrace string) (file string, line int) {
	r := regexp.MustCompile("^(.+):(\\d+)")
	if m := r.FindStringSubmatch(backtrace); len(m) > 0 {
		file = m[1]
		line, _ = strconv.Atoi(m[2])
	}

	return
}

func indent(s string, spaces int) string {
	endl := regexp.MustCompile("^|(\n)")
	sp := "$1" + strings.Repeat(" ", spaces)
	return endl.ReplaceAllString(s, sp)
}

func maybePlural(n int, singular string, plural string) string {
	if n == 1 {
		return singular
	}

	return plural
}

func round(d, r time.Duration) time.Duration {
	if r <= 0 {
		return d
	}
	neg := d < 0
	if neg {
		d = -d
	}
	if m := d % r; m+m < r {
		d = d - m
	} else {
		d = d + r - m
	}
	if neg {
		return -d
	}
	return d
}

func millisDuration(seconds float64) time.Duration {
	return time.Duration(seconds*1000) * time.Millisecond
}

func (self *Pretty) SuiteFinish(final tapjio.SuiteFinishEvent) error {
	self.timeCop.SuiteFinish(final)
	self.clearSummary()

	counts := final.Counts

	// If there are errors/fails don't show any SLOW PASSes
	if self.ShowSnails {
		numSlowOutcomes := len(self.timeCop.SlowPassingOutcomes)
		if self.timeCop.Passed() && numSlowOutcomes > 0 {

			if !self.mostRecentTestPrintedSpacingNewline {
				fmt.Fprintf(self.writer, "\n")
			}

			// Reverse the outcome order so the slowest is at the bottom.
			slowPassingOutcomes := make([]analysis.Outcome, numSlowOutcomes)
			for ix, outcome := range self.timeCop.SlowPassingOutcomes {
				slowPassingOutcomes[numSlowOutcomes - (ix + 1)] = outcome
			}

			for _, outcome := range slowPassingOutcomes {
				fmt.Fprintf(self.writer, "🐌  %-59s %v\n",
					outcome.Label,
					self.yellow(millisDuration(outcome.Duration)))
			}

			// NOTE(adamb) Job time only == real time if jobs == 1. This is because
			//     jobs execute in parallel, thus 2 seconds of job time might only
			//     take 1 second of real time if split equally between two jobs.
			fmt.Fprintf(self.writer,
				self.boldYellow("\nThe %d slowest %s above took %v or %.f%% of job time (the %d %s %v)\n\n"),
				numSlowOutcomes,
				maybePlural(numSlowOutcomes, "test", "tests"),
				millisDuration(self.timeCop.TotalSlowPassingDuration),
				self.timeCop.TotalSlowPassingDuration/self.timeCop.TotalDuration*100,
				len(self.timeCop.FastPassingOutcomes),
				maybePlural(len(self.timeCop.FastPassingOutcomes), "other took", "others each took ≤"),
				millisDuration(self.timeCop.SlowestFastPassingDuration))
		}
	}

	fmt.Fprintf(self.writer, "🏁  Ran %d tests in %v (%v of job time): %s.\n",
		counts.Total,
		millisDuration(final.Time),
		millisDuration(self.timeCop.TotalDuration),
		self.summarizeTally(*counts))

	return nil
}

func (t *Pretty) End(reason error) error {
	return nil
}
