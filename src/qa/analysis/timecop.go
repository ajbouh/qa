package analysis

import (
	"qa/tapjio"
	"sort"
)

// TimeCop detects which tests are "holding back" a particular TAP-J stream.
// It does this by identifying tests are dramatically longer than the average.
type TimeCop struct {
	outcomes               []Outcome
	passingOutcomes        []Outcome
	FastPassingOutcomes    []Outcome
	SlowPassingOutcomes    []Outcome
	averagePassingDuration float64
	totalPassingDuration   float64

	TotalDuration              float64
	TotalSlowPassingDuration   float64
	ThresholdDuration          float64
	SlowestFastPassingDuration float64
	negativeOutcomes           []Outcome

	MaxResults int
}

type ByOutcomeDuration []Outcome

func (a ByOutcomeDuration) Len() int           { return len(a) }
func (a ByOutcomeDuration) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByOutcomeDuration) Less(i, j int) bool { return a[i].Duration < a[j].Duration }

type Outcome struct {
	result   tapjio.Status
	Duration float64
	Label    string
}

func (self *TimeCop) TestFinish(test tapjio.TestFinishEvent) {
	o := Outcome{
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

func (self *TimeCop) SuiteFinish(final tapjio.SuiteFinishEvent) {
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

		sort.Sort(sort.Reverse(ByOutcomeDuration(self.SlowPassingOutcomes)))

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
