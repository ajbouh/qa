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

func NewStyle() *Style {
	return &Style{
		PreferOnlyUserFrames: true,
		ShowAllInternalFrames: false,

		passStyle:  color.New(color.FgGreen, color.Bold).SprintfFunc(),
		failStyle:  color.New(color.FgRed, color.Bold).SprintfFunc(),
		errorStyle: color.New(color.FgMagenta, color.Bold).SprintfFunc(),
		todoStyle:  color.New(color.FgCyan, color.Bold).SprintfFunc(),
		omitStyle:  color.New(color.FgCyan, color.Bold).SprintfFunc(),

		testDescriptionStyle: color.New(color.Bold).SprintfFunc(),

		PassNounSingular: "pass",
		PassNounPlural:   "passes",

		FailNounSingular: "fail",
		FailNounPlural:   "fails",

		ErrorNounSingular: "error",
		ErrorNounPlural:   "errors",

		SkipNounSingular: "skip",
		SkipNounPlural:   "skips",

		OmitNounSingular: "omit",
		OmitNounPlural:   "omits",

		snailSummaryStyle:  color.New(color.Bold, color.FgYellow).SprintFunc(),
		snailDurationStyle: color.New(color.FgYellow).SprintfFunc(),

		lineNumberBlurredStyle: color.New(color.FgBlack, color.Bold).SprintfFunc(),
		lineNumberFocusedStyle: color.New().SprintfFunc(),
		lineNumberMarkerStyle: color.New(color.Bold).SprintfFunc(),
		lineTextFocusedStyle: color.New().SprintfFunc(),

		outputTitleStyle: color.New(color.FgBlack, color.Bold).SprintfFunc(),
	}
}

type Style struct {
	todoStyle   func(s string, a ...interface{}) string
	passStyle   func(s string, a ...interface{}) string
	failStyle   func(s string, a ...interface{}) string
	errorStyle  func(s string, a ...interface{}) string
	omitStyle  func(s string, a ...interface{}) string

	ShowAllInternalFrames bool
	PreferOnlyUserFrames  bool

	PassNounSingular  string
	PassNounPlural    string

	FailNounSingular  string
	FailNounPlural    string

	ErrorNounSingular  string
	ErrorNounPlural    string

	SkipNounSingular  string
	SkipNounPlural    string

	OmitNounSingular  string
	OmitNounPlural    string

	snailDurationStyle func(s string, a ...interface{}) string
	snailSummaryStyle  func(a ...interface{}) string

	testDescriptionStyle func(s string, a ...interface{}) string

	lineNumberBlurredStyle func(s string, a ...interface{}) string
	lineNumberFocusedStyle func(s string, a ...interface{}) string
	lineNumberMarkerStyle func(s string, a ...interface{}) string
	lineTextFocusedStyle func(s string, a ...interface{}) string

	outputTitleStyle func(s string, a ...interface{}) string
}

func (self *Style) formatStatus(status tapjio.Status) string {
	switch status {
	case tapjio.Pass:
		return self.passStyle("✔")
	case tapjio.Fail:
		return self.failStyle("✖")
	case tapjio.Error:
		return self.errorStyle("!")
	case tapjio.Omit:
		return self.omitStyle("Ø")
	case tapjio.Todo:
		return self.todoStyle("…")
	default:
		return self.errorStyle("?")
	}
}

func (self *Style) summarizeException(writer io.Writer, includeClass bool, exception tapjio.TestException) {
	message := ""
	if includeClass {
		message = exception.Class + ": "
	}
	message = message + exception.Message

	fmt.Fprintf(writer, "%s\n\n", indent(message, 3))

	// new_bt = bt.take_while { |e| !e['internal'] }
	// new_bt = bt.select     { |e| !e['internal'] } if new_bt.empty?
	// new_bt = bt.dup                               if new_bt.empty?
	showAllInternalFrames := self.ShowAllInternalFrames
	preferOnlyUserFrames := self.PreferOnlyUserFrames
	stopOnFirstInternal := false
	if !showAllInternalFrames {
		// There's at least one non-internal frame on the top of the stack.
		// Show only the frames until the first internal one.
		if len(exception.Backtrace) > 0 && !exception.Backtrace[0].Internal {
			stopOnFirstInternal = true
		} else {
			anyNonInternal := false
			for _, frame := range exception.Backtrace {
				if !frame.Internal {
					anyNonInternal = true
					break
				}
			}
			showAllInternalFrames = !anyNonInternal
		}

		if preferOnlyUserFrames {
			anyUserFrames := false
			for _, frame := range exception.Backtrace {
				if frame.User {
					anyUserFrames = true
					break
				}
			}
			preferOnlyUserFrames = anyUserFrames
		}
	}

	snippets := exception.Snippets
	for _, entry := range exception.Backtrace {
		if !showAllInternalFrames {
			if preferOnlyUserFrames && !entry.User {
				continue
			}

			if entry.Internal {
				if stopOnFirstInternal {
					break
				} else {
					continue
				}
			}
		}

		fmt.Fprintf(writer, "   File:   %s:%d\n", entry.File, entry.Line)
		vars := entry.Variables
		if len(vars) > 0 {
			fmt.Fprintf(writer, "   Locals:\n")

			maxVarNameLength := 0
			varNames := make([]string, len(vars))
			varIx := 0
			for varName, _ := range vars {
				varNames[varIx] = varName
				varIx++
				l := len(varName)
				if l > maxVarNameLength {
					maxVarNameLength = l
				}
			}

			format := "      %- " + strconv.Itoa(maxVarNameLength) + "s = %s\n"
			sort.Strings(varNames)
			for _, varName := range varNames {
				varValue := vars[varName]
				singleLineValue := strings.Replace(varValue, "\n", "↩ ", -1)
				fmt.Fprintf(writer, format, varName, singleLineValue)
			}
		}

		lines, ok := snippets[entry.File]
		if !ok {
			continue
		}

		// Only show Source: header if we're printing locals, to avoid variables bleeding
		// into source code snippet.
		if len(vars) > 0 {
			fmt.Fprintf(writer, "   Source:\n")
		}

		contextLines := 3
		initialLine := entry.Line - contextLines
		lastLine := entry.Line + contextLines
		if initialLine < 0 {
			initialLine = 1
		}

		for i := initialLine; i <= lastLine; i++ {
			iText := strconv.Itoa(i)
			lineText, ok := lines[iText]
			if !ok {
				continue
			}
			var marker string
			format := "    %s % 3s  %s\n"
			if i == entry.Line {
				marker = self.lineNumberMarkerStyle("›")
				iText = self.lineTextFocusedStyle(iText)
			} else {
				iText = self.lineNumberBlurredStyle(iText)
				marker = " "
			}
			fmt.Fprintf(writer, format, marker, iText, lineText)
		}
	}
}

func (self *Style) summarizeCapturedOutput(writer io.Writer, label, output string) {
	endl := "\n"
	if strings.HasSuffix(output, "\n") {
		endl = ""
	}
	fmt.Fprintf(writer, "   %s\n%s%s",
		self.outputTitleStyle(label),
		indent(output, 3),
		endl)
}

func (self *Style) FormatTestDescription(description string) string {
	return self.testDescriptionStyle(description)
}

func (self *Style) FormatTestExceptionBriefly(event tapjio.TestFinishEvent) string {
	if event.Exception == nil {
		return ""
	}

	e := *event.Exception
	s := strings.Replace(e.Message, "\n", "↩ ", -1)
	if event.Status == tapjio.Error {
		return self.errorStyle(e.Class + ": " + s)
	} else {
		return self.failStyle(s)
	}
}

func (self *Style) SummarizeTestFinish(writer io.Writer, totalTests int, tally tapjio.ResultTally, event tapjio.TestFinishEvent) {
	description := tapjio.TestLabel(event.Label, event.Cases)

	fmt.Fprintf(writer, "%s  %-50s [%d/%d] %v\n",
		self.formatStatus(event.Status),
		self.testDescriptionStyle(description),
		tally.Total, totalTests,
		millisDuration(event.Time))

	if event.Status == tapjio.Todo && event.Exception != nil {
		fmt.Fprintf(writer, "%s\n\n", indent(event.Exception.Message, 3))
	}

	if (event.Status == tapjio.Fail || event.Status == tapjio.Error) && event.Exception != nil {
		self.summarizeException(writer, event.Status == tapjio.Error, *event.Exception)
		fmt.Fprintf(writer, "\n")
	}

	if event.Stdout != "" {
		self.summarizeCapturedOutput(writer, "STDOUT", event.Stdout)
	}

	if event.Stderr != "" {
		self.summarizeCapturedOutput(writer, "STDERR", event.Stderr)
	}
}

func (self *Style) SummarizeSnails(writer io.Writer, timecop *analysis.TimeCop) {
	numSlowOutcomes := len(timecop.SlowPassingOutcomes)
	if timecop.Passed() && numSlowOutcomes > 0 {

		// Reverse the outcome order so the slowest is at the bottom.
		slowPassingOutcomes := make([]analysis.Outcome, numSlowOutcomes)
		for ix, outcome := range timecop.SlowPassingOutcomes {
			slowPassingOutcomes[numSlowOutcomes-(ix+1)] = outcome
		}

		for _, outcome := range slowPassingOutcomes {
			fmt.Fprintf(writer, "🐌  %-59s %v\n",
				outcome.Label,
				self.snailDurationStyle("%s", millisDuration(outcome.Duration)))
		}

		// NOTE(adamb) Job time only == real time if jobs == 1. This is because
		//     jobs execute in parallel, thus 2 seconds of job time might only
		//     take 1 second of real time if split equally between two jobs.
		fmt.Fprintf(writer,
			self.snailSummaryStyle("\nThe %d slowest %s above took %v or %.f%% of job time (the %d %s %v)\n\n"),
			numSlowOutcomes,
			MaybePlural(numSlowOutcomes, "test", "tests"),
			millisDuration(timecop.TotalSlowPassingDuration),
			timecop.TotalSlowPassingDuration/timecop.TotalDuration*100,
			len(timecop.FastPassingOutcomes),
			MaybePlural(len(timecop.FastPassingOutcomes), "other took", "others each took ≤"),
			millisDuration(timecop.SlowestFastPassingDuration))
	}
}

func (self *Style) FormatTally(tally tapjio.ResultTally) string {
	countLabels := []string{}
	if tally.Pass > 0 {
		countLabels = append(countLabels,
			self.passStyle("%d %s", tally.Pass, MaybePlural(tally.Pass, self.PassNounSingular, self.PassNounPlural)))
	}

	if tally.Fail > 0 {
		countLabels = append(countLabels,
			self.failStyle("%d %s", tally.Fail, MaybePlural(tally.Fail, self.FailNounSingular, self.FailNounPlural)))
	}

	if tally.Error > 0 {
		countLabels = append(countLabels,
			self.errorStyle("%d %s", tally.Error, MaybePlural(tally.Error, self.ErrorNounSingular, self.ErrorNounPlural)))
	}

	if tally.Todo > 0 {
		countLabels = append(countLabels,
			self.todoStyle("%d %s", tally.Todo, MaybePlural(tally.Todo, self.SkipNounSingular, self.SkipNounPlural)))
	}

	if tally.Omit > 0 {
		countLabels = append(countLabels,
			self.omitStyle("%d %s", tally.Omit, MaybePlural(tally.Omit, self.OmitNounSingular, self.OmitNounPlural)))
	}

	return strings.Join(countLabels, ", ")
}

func MaybePlural(n int, singular string, plural string) string {
	if n == 1 {
		return singular
	}

	return plural
}

func indent(s string, spaces int) string {
	endl := regexp.MustCompile("^|(\n)")
	sp := "$1" + strings.Repeat(" ", spaces)
	return endl.ReplaceAllString(s, sp)
}

func Round(d, r time.Duration) time.Duration {
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
