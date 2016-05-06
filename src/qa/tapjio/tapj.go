package tapjio

import (
	"encoding/json"
	"fmt"
	"io"
)

type tapj struct {
	writer       io.Writer
	currentCases []CaseEvent
}

func NewTapjEmitter(writer io.Writer) *tapj {
	return &tapj{writer: writer}
}

func (t *tapj) TraceEvent(event TraceEvent) error {
	var s []byte
	var err error

	s, err = json.Marshal(event)
	if err != nil {
		return err
	}

	fmt.Fprintln(t.writer, string(s))
	return nil
}

func (t *tapj) SuiteStarted(event SuiteEvent) error {
	var s []byte
	var err error

	s, err = json.Marshal(event)
	if err != nil {
		return err
	}

	fmt.Fprintln(t.writer, string(s))
	return nil
}

func (t *tapj) TestStarted(event TestEvent) error {
	// Since this isn't properly represented by TAP-J, no need to emit it!
	return nil
}

func (t *tapj) TestFinished(event TestEvent) error {
	var err error

	prefixMatches := true
	for i, kase := range event.Cases {
		if prefixMatches && i < len(t.currentCases) && kase == t.currentCases[i] {
			continue
		} else {
			prefixMatches = false
		}

		var s []byte
		s, err = json.Marshal(kase)
		if err != nil {
			return err
		}
		fmt.Fprintln(t.writer, string(s))
	}
	t.currentCases = event.Cases

	var s []byte
	s, err = json.Marshal(event)
	if err != nil {
		return err
	}

	fmt.Fprintln(t.writer, string(s))
	return nil
}

func (t *tapj) SuiteFinished(event FinalEvent) error {
	var s []byte
	var err error

	s, err = json.Marshal(event)
	if err != nil {
		return err
	}

	fmt.Fprintln(t.writer, string(s))
	return nil
}
