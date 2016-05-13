package analysis

import (
	"qa/tapjio"
)

type TimeCop struct {
	outcomes               []outcome
	passingOutcomes        []outcome
	FastPassingOutcomes    []outcome
	SlowPassingOutcomes    []outcome
	averagePassingDuration float64
	totalPassingDuration   float64

	TotalDuration              float64
	TotalSlowPassingDuration   float64
	ThresholdDuration          float64
	SlowestFastPassingDuration float64
	negativeOutcomes           []outcome

	MaxResults             int
}

type ByOutcomeDuration []outcome
func (a ByOutcomeDuration) Len() int           { return len(a) }
func (a ByOutcomeDuration) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByOutcomeDuration) Less(i, j int) bool { return a[i].Duration < a[j].Duration }

type outcome struct {
	result   tapjio.Status
	Duration float64
	Label    string
}

func (self *TimeCop) TestFinished(test tapjio.TestEvent) {
	o := outcome{
		Duration: test.Time,
		Label:    tapjio.TestLabel(test.Label, test.Cases),
		result:   test.Status,
	}

	self.TotalDuration += test.Time

	self.outcomes = append(self.outcomes, o)
	if o.result == "pass" {
		self.totalPassingDuration += o.Duration
		self.passingOutcomes = append(self.passingOutcomes, o)
	} else if o.result == "fail" || o.result == "error" {
		self.negativeOutcomes = append(self.negativeOutcomes, o)
	}
}

func (self *TimeCop) SuiteFinished(final tapjio.FinalEvent) {
	// Assuming we have any, compute average and threshold durations.
	if len(self.passingOutcomes) > 0 {
		self.averagePassingDuration = self.totalPassingDuration / float64(len(self.passingOutcomes))
		self.ThresholdDuration = self.averagePassingDuration * 2.0
		self.TotalSlowPassingDuration = 0

		for _, passingOutcome := range self.passingOutcomes {
			if passingOutcome.Duration < self.ThresholdDuration {
				self.FastPassingOutcomes = append(self.FastPassingOutcomes, passingOutcome)
				if passingOutcome.Duration > self.SlowestFastPassingDuration {
					self.SlowestFastPassingDuration = passingOutcome.Duration
				}
			} else {
				self.SlowPassingOutcomes = append(self.SlowPassingOutcomes, passingOutcome)
				self.TotalSlowPassingDuration += passingOutcome.Duration
			}
		}

		if self.MaxResults >= 0 && len(self.SlowPassingOutcomes) > self.MaxResults {
			sparedPassingOutcomes := self.SlowPassingOutcomes[self.MaxResults:]
			self.SlowPassingOutcomes = self.SlowPassingOutcomes[:self.MaxResults]
			for _, sparedPassingOutcome := range sparedPassingOutcomes {
				if sparedPassingOutcome.Duration > self.SlowestFastPassingDuration {
					self.SlowestFastPassingDuration = sparedPassingOutcome.Duration
				}
				self.TotalSlowPassingDuration -= sparedPassingOutcome.Duration
			}

			self.FastPassingOutcomes = append(self.FastPassingOutcomes, sparedPassingOutcomes...)
		}
	}
}

func (self *TimeCop) Passed() bool {
	return len(self.negativeOutcomes) == 0
}
