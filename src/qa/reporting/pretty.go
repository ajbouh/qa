package reporting

import (
	"fmt"
	"io"
	"qa/analysis"
	"qa/tapjio"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

type Pretty struct {
	writer        io.Writer
	jobs          int
	lastCase      *tapjio.CaseEvent
	startTime     time.Time
	totalTests    int
	totalTestTime float64
	seed          int
	timeCop       *analysis.TimeCop
	tally         *tapjio.ResultTally

	pending       map[string]string

	boldYellow    func(a ...interface{}) string
	cyan          func(a ...interface{}) string
	green         func(a ...interface{}) string
	red           func(a ...interface{}) string
	magenta       func(a ...interface{}) string
	bold          func(a ...interface{}) string
}

func NewPretty(writer io.Writer, jobs int) *Pretty {
	return &Pretty {
		writer: writer,
		jobs: jobs,
		pending: make(map[string]string),
		timeCop: &analysis.TimeCop{MaxResults: 10},
		tally: &tapjio.ResultTally{},
		boldYellow: color.New(color.Bold, color.FgYellow).SprintFunc(),
		cyan: color.New(color.FgCyan).SprintFunc(),
		green: color.New(color.FgGreen).SprintFunc(),
		red: color.New(color.FgRed).SprintFunc(),
		magenta: color.New(color.FgMagenta).SprintFunc(),
		bold: color.New(color.Bold).SprintFunc(),
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

func (self *Pretty) SuiteStarted(suite tapjio.SuiteEvent) error {
	self.totalTests = suite.Count
	self.seed = suite.Seed
	self.startTime = time.Now()
	fmt.Fprintf(self.writer, "Will run %d tests using %d %s and seed %d.\n\n",
		self.totalTests,
		self.jobs,
		maybePlural(self.jobs, "job", "jobs"),
		self.seed)

	self.writeSummary()
	return nil
}

func (self *Pretty) clearSummary() {
	n := 2 + len(self.pending)
	for i := 0; i < n; i++ {
		// Move up, move to beginning of line, clear line
		fmt.Fprintf(self.writer, "\033[1A\r\033[2K")
	}
}

func (self *Pretty) writeSummary() {
	numPending := len(self.pending)

	tallySummaryPrefix := ""
	tallySummary := self.summarizeTally(*self.tally)
	if tallySummary != "" {
		tallySummaryPrefix = ": "
	}

	fmt.Fprintf(self.writer, "\nRan %d%% in %v (%v of job time)%s%s. %d remaining, %d running:\n",
		int(float64(self.tally.Total) / float64(self.totalTests) * 100.0),
		round(time.Since(self.startTime), time.Millisecond),
		millisDuration(self.totalTestTime),
		tallySummaryPrefix,
		tallySummary,
		self.totalTests - self.tally.Total - numPending,
		numPending)

	for _, label := range self.pending {
		fmt.Fprintf(self.writer, "  %s\n", self.bold(label))
	}
}

func (self *Pretty) TestStarted(event tapjio.TestStartedEvent) error {
	self.clearSummary()
	self.pending[event.Filter] = tapjio.TestLabel(event.Label, event.Cases)
	self.writeSummary()

	return nil
}

func (self *Pretty) TestFinished(test tapjio.TestEvent) error {
	self.timeCop.TestFinished(test)
	self.tally.Increment(test.Status)

	self.totalTestTime += test.Time

	label := tapjio.TestLabel(test.Label, test.Cases)
	self.clearSummary()
	delete(self.pending, test.Filter)
	defer self.writeSummary()

	if (test.Status == tapjio.Pass || test.Status == tapjio.Omit) &&
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
		status = self.magenta("‼") // "ERROR"
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

	if test.Exception != nil {
		// fmt.Fprintf(self.writer, "%s\n\n", indent(test.Exception.Class, 3))
		fmt.Fprintf(self.writer, "%s\n\n", indent(test.Exception.Message, 3))
		// puts backtrace_snippets(test).indent(tabsize)

		// for index, lineMap := range test.Exception.Snippet {
		// fmt.Fprintf(self.writer, "%d:%v   %v\n", index, lineMap, test.Exception.Backtrace)
		// backtrace := test.Exception.Backtrace[index]
		// backtraceFile, backtraceLine := parseBacktracePosition(backtrace)
		// fmt.Fprintf(self.writer, "%s:%d\n", backtraceFile, backtraceLine)
		// for lineNum, lineText := range lineMap {
		// 	fmt.Fprintf(self.writer, "%s: %s\n", lineNum, lineText)
		// }
		// }
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
	return time.Duration(seconds * 1000) * time.Millisecond
}

func (self *Pretty) SuiteFinished(final tapjio.FinalEvent) error {
	self.timeCop.SuiteFinished(final)
	self.clearSummary()

	counts := final.Counts

	fmt.Fprintf(self.writer, "\n🏁  Ran %d tests in %v (%v of job time): %s.\n",
		counts.Total,
		millisDuration(final.Time),
		millisDuration(self.timeCop.TotalDuration),
		self.summarizeTally(counts))

	// If there are errors/fails don't show any SLOW PASSes
	if self.timeCop.Passed() && len(self.timeCop.SlowPassingOutcomes) > 0 {
		fmt.Fprintf(self.writer, "\n")

		// NOTE(adamb) Job time only == real time if jobs == 1. This is because
		//     jobs execute in parallel, thus 2 seconds of job time might only
		//     take 1 second of real time if split equally between two jobs.
		fmt.Fprintf(self.writer,
			self.boldYellow("The %d slowest %s took %v or %.f%% of job time (the %d %s %v):\n"),
			len(self.timeCop.SlowPassingOutcomes),
			maybePlural(len(self.timeCop.SlowPassingOutcomes), "test", "tests"),
			millisDuration(self.timeCop.TotalSlowPassingDuration),
			self.timeCop.TotalSlowPassingDuration / self.timeCop.TotalDuration * 100,
			len(self.timeCop.FastPassingOutcomes),
			maybePlural(len(self.timeCop.FastPassingOutcomes), "other took", "others each took ≤"),
			millisDuration(self.timeCop.SlowestFastPassingDuration))

		slowPassingOutcomes := self.timeCop.SlowPassingOutcomes
		sort.Sort(sort.Reverse(analysis.ByOutcomeDuration(slowPassingOutcomes)))
		for _, outcome := range slowPassingOutcomes {
			fmt.Fprintf(self.writer, "🐌  %-59s %v\n",
					self.boldYellow(outcome.Label),
					millisDuration(outcome.Duration))
		}
	}

	return nil
}
