package reporters

import (
	"fmt"
	"io"
	"qa/tapj"
	"qa/tapj/analysis"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

type Pretty struct {
	Writer        io.Writer
	Jobs          int
	lastCase      *tapj.CaseEvent
	totalTests    int
	finishedTests int
	timeCop       analysis.TimeCop
}

func (self *Pretty) SuiteStarted(suite tapj.SuiteEvent) error {
	self.totalTests = suite.Count
	self.finishedTests = 0
	fmt.Fprintf(self.Writer, "Running suite of %d tests (jobs: %d, seed: %d)\n\n",
		suite.Count,
		self.Jobs,
		suite.Seed)

	return nil
}

func (self *Pretty) TestFinished(cases []tapj.CaseEvent, test tapj.TestEvent) error {
	self.timeCop.TestFinished(cases, test)

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	label := tapj.TestLabel(cases, test)
	var status string
	switch test.Status {
	case tapj.Pass:
		status = green("✔") // " PASS"
	case tapj.Fail:
		status = red("✖") // " FAIL"
	case tapj.Error:
		status = magenta("‼") // "ERROR"
	case tapj.Omit:
		status = cyan("Ø") // " OMIT"
	case tapj.Todo:
		status = cyan("…") // " SKIP"
	default:
		status = red("?")
	}

	self.finishedTests += 1

	description := fmt.Sprintf("%s", bold(label))

	fmt.Fprintf(self.Writer, "%s  %-50s [%d/%d] %.3fs\n",
		status,
		description,
		self.finishedTests, self.totalTests,
		test.Time)

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

func (self *Pretty) SuiteFinished(suite tapj.SuiteEvent, final tapj.FinalEvent) error {
	self.timeCop.SuiteFinished(suite, final)

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

	fmt.Fprintf(self.Writer, "\nFinished %d tests in %.3f seconds: %s.\n",
		counts.Total,
		final.Time,
		strings.Join(countLabels, ", "))

	// If there are errors/fails don't show any SLOW PASSes
	if self.timeCop.Passed() && len(self.timeCop.SlowPassingOutcomes) > 0 {
		fmt.Fprintf(self.Writer, "\n")

		fmt.Fprintf(self.Writer,
			boldYellow("%d %s took > %.3fs to pass (the %d %s took %.3fs or less):\n"),
			len(self.timeCop.SlowPassingOutcomes),
			maybePlural(len(self.timeCop.SlowPassingOutcomes), "test", "tests"),
			self.timeCop.ThresholdDuration,
			len(self.timeCop.FastPassingOutcomes),
			maybePlural(len(self.timeCop.FastPassingOutcomes), "other", "others"),
			self.timeCop.SlowestFastPassingDuration)

		for _, outcome := range self.timeCop.SlowPassingOutcomes {
			fmt.Fprintf(self.Writer, "🐌  %-59s %.3fs\n", boldYellow(outcome.Label), outcome.Duration)
		}
	}

	return nil
}
