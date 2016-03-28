package analysis

import (
	"qa/tapj"
)

type TimeCop struct {
	outcomes               []outcome
	passingOutcomes        []outcome
	FastPassingOutcomes    []outcome
	SlowPassingOutcomes    []outcome
	averagePassingDuration float64
	totalPassingDuration   float64

	ThresholdDuration          float64
	SlowestFastPassingDuration float64
	negativeOutcomes           []outcome
}

type outcome struct {
	result   tapj.Status
	Duration float64
	Label    string
}

func (self *TimeCop) TestFinished(cases []tapj.CaseEvent, test tapj.TestEvent) {
	o := outcome{
		Duration: test.Time,
		Label:    tapj.TestLabel(cases, test),
		result:   test.Status,
	}

	self.outcomes = append(self.outcomes, o)
	if o.result == "pass" {
		self.totalPassingDuration += o.Duration
		self.passingOutcomes = append(self.passingOutcomes, o)
	} else if o.result == "fail" || o.result == "error" {
		self.negativeOutcomes = append(self.negativeOutcomes, o)
	}
}

func (self *TimeCop) SuiteFinished(suite tapj.SuiteEvent, final tapj.FinalEvent) {
	// Assuming we have any, compute average and threshold durations.
	if len(self.passingOutcomes) > 0 {
		self.averagePassingDuration = self.totalPassingDuration / float64(len(self.passingOutcomes))
		self.ThresholdDuration = self.averagePassingDuration * 1.10

		for _, passingOutcome := range self.passingOutcomes {
			if passingOutcome.Duration < self.ThresholdDuration {
				self.FastPassingOutcomes = append(self.FastPassingOutcomes, passingOutcome)
				if passingOutcome.Duration > self.SlowestFastPassingDuration {
					self.SlowestFastPassingDuration = passingOutcome.Duration
				}
			} else {
				self.SlowPassingOutcomes = append(self.SlowPassingOutcomes, passingOutcome)
			}
		}
	}
}

func (self *TimeCop) Passed() bool {
	return len(self.negativeOutcomes) == 0
}
