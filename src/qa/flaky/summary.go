package flaky

import (
	"encoding/json"
	"qa/tapjio"
)

type IntByOutcomeDigest map[tapjio.OutcomeDigest]int
type FloatByOutcomeDigest map[tapjio.OutcomeDigest]float64
type StatusByOutcomeDigest map[tapjio.OutcomeDigest]tapjio.Status
type StringByOutcomeDigest map[tapjio.OutcomeDigest]string

type TestSummary struct {
	Id              interface{}            `json:"id"`
	Description     string                 `json:"description"`
	TotalCount      int                    `json:"total-count"`
	TotalDuration   float64                `json:"total-duration"`
	PassCount       int                    `json:"pass-count"`
	FailCount       int                    `json:"fail-count"`
	OutcomeSequence string                 `json:"outcome-sequence"`
	OutcomeDigests  []tapjio.OutcomeDigest `json:"outcome-digests"`

	Prototypes map[tapjio.OutcomeDigest]tapjio.TestFinishEvent `json:"prototype"`

	Statuses                    StatusByOutcomeDigest `json:"status"`
	Counts                      IntByOutcomeDigest    `json:"count"`
	Probability                 FloatByOutcomeDigest  `json:"probability"`
	ReproRunLimitProbabilities  FloatByOutcomeDigest  `json:"repro-limit-probability"`
	ReproRunsLimits             IntByOutcomeDigest    `json:"repro-run-limit"`
	ReproLimitExpectDurations FloatByOutcomeDigest  `json:"repro-limit-expected-duration"`

	Mean                 FloatByOutcomeDigest  `json:"mean"`
	Min                  FloatByOutcomeDigest  `json:"min"`
	Median               FloatByOutcomeDigest  `json:"median"`
	Max                  FloatByOutcomeDigest  `json:"max"`
	Shorthand            StringByOutcomeDigest `json:"outcome-index"`
}

func DecodeSummary(decoder *json.Decoder) (*TestSummary, error) {
	r := &TestSummary{}
	err := decoder.Decode(r)
	return r, err
}

func (s TestSummary) FindOutcomeDigest(shorthand string) (tapjio.OutcomeDigest, bool) {
	for outcome, s := range s.Shorthand {
		if s == shorthand {
			return outcome, true
		}
	}

	return tapjio.NoOutcome, false
}
