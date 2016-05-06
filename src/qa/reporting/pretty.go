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
	Writer        io.Writer
	Jobs          int
	lastCase      *tapjio.CaseEvent
	totalTests    int
	finishedTests int
	timeCop       analysis.TimeCop
}

func (self *Pretty) Event(event interface{}) error {
	return nil
}

func (self *Pretty) TraceEvent(trace tapjio.TraceEvent) error {
	return nil
}

func (self *Pretty) SuiteStarted(suite tapjio.SuiteEvent) error {
	self.totalTests = suite.Count
	self.finishedTests = 0
	fmt.Fprintf(self.Writer, "Running suite of %d tests (jobs: %d, seed: %d)\n\n",
		suite.Count,
		self.Jobs,
		suite.Seed)

	return nil
}

func (self *Pretty) TestStarted(test tapjio.TestEvent) error {
	return nil
}

func (self *Pretty) TestFinished(test tapjio.TestEvent) error {
	self.timeCop.TestFinished(test)

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	label := tapjio.TestLabel(test)
	var status string
	switch test.Status {
	case tapjio.Pass:
		status = green("✔") // " PASS"
	case tapjio.Fail:
		status = red("✖") // " FAIL"
	case tapjio.Error:
		status = magenta("‼") // "ERROR"
	case tapjio.Omit:
		status = cyan("Ø") // " OMIT"
	case tapjio.Todo:
		status = cyan("…") // " SKIP"
	default:
		status = red("?")
	}

	self.finishedTests += 1

	description := fmt.Sprintf("%s", bold(label))

	fmt.Fprintf(self.Writer, "%s  %-50s [%d/%d] %v\n",
		status,
		description,
		self.finishedTests, self.totalTests,
		millisDuration(test.Time))

	if test.Exception != nil {
		// fmt.Fprintf(self.Writer, "%s\n\n", indent(test.Exception.Class, 3))
		fmt.Fprintf(self.Writer, "%s\n\n", indent(test.Exception.Message, 3))
		// puts backtrace_snippets(test).indent(tabsize)

		// for index, lineMap := range test.Exception.Snippet {
		// fmt.Fprintf(self.Writer, "%d:%v   %v\n", index, lineMap, test.Exception.Backtrace)
		// backtrace := test.Exception.Backtrace[index]
		// backtraceFile, backtraceLine := parseBacktracePosition(backtrace)
		// fmt.Fprintf(self.Writer, "%s:%d\n", backtraceFile, backtraceLine)
		// for lineNum, lineText := range lineMap {
		// 	fmt.Fprintf(self.Writer, "%s: %s\n", lineNum, lineText)
		// }
		// }
	}

	if test.Stdout != "" {
		endl := "\n"
		if strings.HasSuffix(test.Stdout, "\n") {
			endl = ""
		}
		fmt.Fprintf(self.Writer, "   %s\n%s%s\n", "STDOUT", indent(test.Stdout, 3), endl)
	}

	if test.Stderr != "" {
		endl := "\n"
		if strings.HasSuffix(test.Stderr, "\n") {
			endl = ""
		}
		fmt.Fprintf(self.Writer, "   %s\n%s%s\n", "STDERR", indent(test.Stderr, 3), endl)
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

func millisDuration(seconds float64) time.Duration {
	return time.Duration(seconds * 1000) * time.Millisecond
}

func (self *Pretty) SuiteFinished(final tapjio.FinalEvent) error {
	self.timeCop.SuiteFinished(final)

	boldYellow := color.New(color.Bold, color.FgYellow).SprintFunc()

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()

	counts := final.Counts
	countLabels := []string{}
	if counts.Pass > 0 {
		countLabels = append(countLabels,
			green(counts.Pass, maybePlural(counts.Pass, " pass", " passes")))
	}

	if counts.Fail > 0 {
		countLabels = append(countLabels,
			red(counts.Fail, maybePlural(counts.Fail, " fail", " fails")))
	}

	if counts.Error > 0 {
		countLabels = append(countLabels,
			magenta(counts.Error, maybePlural(counts.Error, " error", " errors")))
	}

	if counts.Todo > 0 {
		countLabels = append(countLabels,
			cyan(counts.Todo, maybePlural(counts.Todo, " skip", " skips")))
	}

	if counts.Omit > 0 {
		countLabels = append(countLabels,
			cyan(counts.Omit, maybePlural(counts.Omit, " omit", " omits")))
	}

	fmt.Fprintf(self.Writer, "\n🏁  Finished %d tests in %v (%v of job time): %s.\n",
		counts.Total,
		millisDuration(final.Time),
		millisDuration(self.timeCop.TotalDuration),
		strings.Join(countLabels, ", "))

	// If there are errors/fails don't show any SLOW PASSes
	if self.timeCop.Passed() && len(self.timeCop.SlowPassingOutcomes) > 0 {
		fmt.Fprintf(self.Writer, "\n")

		// NOTE(adamb) Job time only == real time if jobs == 1. This is because
		//     jobs execute in parallel, thus 2 seconds of job time might only
		//     take 1 second of real time if split equally between two jobs.
		fmt.Fprintf(self.Writer,
			boldYellow("The %d slowest %s took %v or %.f%% of job time (the %d %s %v):\n"),
			len(self.timeCop.SlowPassingOutcomes),
			maybePlural(len(self.timeCop.SlowPassingOutcomes), "test", "tests"),
			millisDuration(self.timeCop.TotalSlowPassingDuration),
			self.timeCop.TotalSlowPassingDuration / self.timeCop.TotalDuration * 100,
			len(self.timeCop.FastPassingOutcomes),
			maybePlural(len(self.timeCop.FastPassingOutcomes), "other took", "others took ≤"),
			millisDuration(self.timeCop.SlowestFastPassingDuration))

		slowPassingOutcomes := self.timeCop.SlowPassingOutcomes
		sort.Sort(sort.Reverse(analysis.ByOutcomeDuration(slowPassingOutcomes)))
		for _, outcome := range slowPassingOutcomes {
			fmt.Fprintf(self.Writer, "🐌  %-59s %v\n",
					boldYellow(outcome.Label),
					millisDuration(outcome.Duration))
		}
	}

	return nil
}
