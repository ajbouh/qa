package tapjio

import (
	"encoding/json"
	"io"
)

type tapj struct {
	closer       io.Closer
	encoder      *json.Encoder
	currentCases []CaseEvent
}

func NewTapjEmitCloser(writer io.WriteCloser) *tapj {
	return &tapj{encoder: json.NewEncoder(writer), closer: writer}
}

func NewTapjEmitter(writer io.Writer) *tapj {
	return &tapj{encoder: json.NewEncoder(writer), closer: nil}
}

func (t *tapj) TraceEvent(event TraceEvent) error {
	return t.encoder.Encode(event)
}

func (t *tapj) SuiteStarted(event SuiteEvent) error {
	return t.encoder.Encode(event)
}

func (t *tapj) ensureTestCases(cases []CaseEvent) error {
	prefixMatches := true
	for i, kase := range cases {
		if prefixMatches && i < len(t.currentCases) && kase == t.currentCases[i] {
			continue
		} else {
			prefixMatches = false
		}

		if err := t.encoder.Encode(kase); err != nil {
			return err
		}
	}
	t.currentCases = cases

	return nil
}

func (t *tapj) TestStarted(event TestStartedEvent) error {
	// Since this isn't properly represented by TAP-J, no need to emit it!
	err := t.ensureTestCases(event.Cases)
	if err != nil {
		return err
	}

	return t.encoder.Encode(event)
}

func (t *tapj) TestFinished(event TestEvent) error {
	err := t.ensureTestCases(event.Cases)
	if err != nil {
		return err
	}

	return t.encoder.Encode(event)
}

func (t *tapj) SuiteFinished(event FinalEvent) error {
	return t.encoder.Encode(event)
}

func (t *tapj) End(reason error) error {
	if t.closer != nil {
		return t.closer.Close()
	}

	return nil
}
