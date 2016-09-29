package repro

import (
	"fmt"
	"math"
	"qa/cmd"
	"qa/cmd/run"
	"qa/flaky"
	"qa/reporting"
	"qa/tapjio"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func round(f float64) int {
	return int(math.Floor(f + .5))
}

func formatDuration(duration time.Duration) string {
	var parts []string

	if hours := duration.Hours(); hours >= 1 {
		parts = append(parts, strconv.Itoa(int(hours)) + "h")
	}

	if min := duration.Minutes(); min >= 1 {
		parts = append(parts, strconv.Itoa(int(min)) + "m")
	}

	if sec := duration.Seconds(); sec >= 1 {
		parts = append(parts, strconv.FormatFloat(sec, 'f', 3, 64) + "s")
	}

	return strings.Join(parts, "")
}

func Main(session *flaky.Session, env *cmd.Env, argv []string) error {
	args := argv[1:]
	if len(args) < 1 {
		return fmt.Errorf("%s: missing outcome name", argv[0])
	}

	re := regexp.MustCompile("^(\\d+)([a-z]*)$")
	outcomeShorthand := args[0]
	parts := re.FindStringSubmatch(outcomeShorthand)
	if len(parts) == 0 {
		return fmt.Errorf("%s: bad reference: %s", argv[0], outcomeShorthand)
	}

	summaryId, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("%s: bad reference: %s", argv[0], outcomeShorthand)
		return err
	}

	summaries, err := session.Summaries()
	if err != nil {
		return err
	}

	if summaryId < 1 || summaryId > len(summaries) {
		return fmt.Errorf("%s: reference must be integer between 1 and %d (inclusive). Got: %s", argv[0], len(summaries), outcomeShorthand)
	}

	summary := summaries[summaryId-1]
	var outcome tapjio.OutcomeDigest
	if len(parts) > 2 && parts[2] != "" {
		var ok bool
		shorthand := parts[2]
		outcome, ok = summary.FindOutcomeDigest(shorthand)
		if !ok {
			return fmt.Errorf("%s: no such outcome: %s", argv[0], shorthand)
		}
	}

	prototype := summary.Prototypes[tapjio.PassDigest]
	runner := prototype.Runner

	var prototypeException tapjio.TestException

	style := reporting.NewStyle()
	style.FailNounSingular = "unrelated fail"
	style.FailNounPlural = "unrelated fails"
	style.ErrorNounSingular = "unrelated error"
	style.ErrorNounPlural = "unrelated errors"
	style.SkipNounSingular = "unrelated skip"
	style.SkipNounPlural = "unrelated skips"
	style.OmitNounSingular = "unrelated omit"
	style.OmitNounPlural = "unrelated omits"

	outcomeMessage := ""

	runs := 1
	runsText := ""
	reproProbability := 0.0

	if outcome != tapjio.NoOutcome {
		prototype = summary.Prototypes[outcome]
		if prototype.Exception != nil {
			prototypeException = *prototype.Exception
			outcomeMessage = style.FormatTestExceptionBriefly(prototype)
		}

		if prototype.Runner != "" {
			runner = prototype.Runner
		}

		runs = summary.ReproRunsLimits[outcome]
		reproProbability = summary.ReproRunLimitProbabilities[outcome]
		runsText = fmt.Sprintf(" up to %d times", runs)
	}

	fmt.Fprintf(env.Stderr, "Running %s%s.\n",
			style.FormatTestDescription(summary.Description),
			runsText)

	if outcomeMessage != "" {
		fmt.Fprintf(env.Stderr, "To reproduce %s: %s\n\n", prototype.Status, outcomeMessage)
	} else {
		fmt.Fprintf(env.Stderr, "\n")
	}

	startTime := time.Now()
	jobDuration := 0.0

	tally := &tapjio.ResultTally{}

	reproductionSucceeded := false
	return run.FrameworkWithVisitor(
		runner,
		env,
		[]string{
			session.ProgramName + " " + runner,
			"-archive", session.ArchiveBaseDir,
			"-runs", strconv.Itoa(runs),
			"-jobs=1",
			"-quiet",
			"-debug-only-outcome", outcome.String(),
			"-done-after-debug=true",
			"-pretty-overwrite=false",
			"-capture-standard-fds=false",
			"-debug-error-class", prototypeException.Class,
			"-debug-errors-with=pry-remote",
			"-filter", prototype.Filter.String(),
			prototype.File.String(),
		},
		&tapjio.DecodingCallbacks{
			OnTestFinish: func(event tapjio.TestFinishEvent) error {
				tally.Increment(event.Status)
				jobDuration += event.Time
				elapsedTime := reporting.Round(time.Now().Sub(startTime), time.Second)

				latestOutcome, err := tapjio.OutcomeDigestFor(event.Status, event.Exception)
				if err != nil {
					return nil
				}

				if outcome != tapjio.NoOutcome {
					// Reproduction after 122 runs, 4s (0s of job time).
					// Attaching debugger before teardown...

					if outcome == latestOutcome {
						fmt.Fprintf(env.Stderr, "\rReproduction in %d %s, %s (%s of job time). Attaching debugger before teardown...\033[K\n",
							tally.Total,
							reporting.MaybePlural(tally.Total, "run", "runs"),
							elapsedTime,
							time.Duration(jobDuration * 1000) * time.Millisecond,
						)

						reproductionSucceeded = true

						return nil
					}
				}

				tallyDescription := ""
				if tally.Total > 0 {
					tallyDescription = fmt.Sprintf(": %s", style.FormatTally(*tally))
				}

				// Ran 12/1654 in 31s (1s job time): 12 passes...
				fmt.Fprintf(env.Stderr, "\rRan %d/%d in %s (%s job time)%s...\033[K",
					tally.Total,
					runs,
					elapsedTime,
					time.Duration(jobDuration * 1000) * time.Millisecond,
					tallyDescription,
				)

				return nil
			},
			OnEnd: func(reason error) error {
				if reproductionSucceeded || tally.Total != runs {
					return nil
				}

				elapsedTime := reporting.Round(time.Now().Sub(startTime), time.Second)

				fmt.Fprintf(env.Stderr, "\rNo reproduction in %d %s, %s (%s of job time).\033[K\n",
					tally.Total,
					reporting.MaybePlural(tally.Total, "run", "runs"),
					elapsedTime,
					time.Duration(jobDuration * 1000) * time.Millisecond,
				)
				if reproProbability != 0.0 {
					fmt.Fprintf(env.Stderr, "There's a %.2f%% chance of this happening by chance. Or maybe it's fixed!\n",
						(1.0 - reproProbability) * 100.0)
				}

				return nil
			},
		},
	)
}
