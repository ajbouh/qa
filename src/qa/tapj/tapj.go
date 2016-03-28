package tapj

//go:generate go-bindata -o $GOGENPATH/qa/runners/bindata.go -pkg runners -prefix ../runner-assets/ ../runner-assets/...

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Status string

const (
	Pass  Status = "pass"
	Todo  Status = "todo"
	Omit  Status = "omit"
	Fail  Status = "fail"
	Error Status = "error"
)

type BaseEvent struct {
	Type string `json:"type"`
}

type SuiteEvent struct {
	Type  string `json:"type"`
	Start string `json:"start"`
	Count int    `json:"count"`
	Seed  int    `json:"seed"`
	Rev   int    `json:"rev"`
}

type CaseEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Label   string `json:"label"`
	Level   int    `json:"level"`
}

type TestException struct {
	Message string `json:"message"`
	Class   string `json:"class"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Source  string `json:"source"`

	// e.g. [
	//  {"3":"class SkipTest < Minitest::Test"},{"4":"  def test_skip"},{"5":"    skip"},{"6":"  end"},{"7":"end"}
	// ],
	Snippet []map[string]string `json:"snippet"`

	// e.g. [
	//  "test/skip-test.rb:5"
	// ]
	Backtrace []string `json:"backtrace"`
}

type TestEvent struct {
	Type    string  `json:"type"`
	Time    float64 `json:"time"`
	Label   string  `json:"label"`
	Subtype string  `json:"subtype"`
	Status  Status  `json:"status"`
	Filter  string  `json:"filter,omitempty"`
	File    string  `json:"file,omitempty"`

	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`

	Exception *TestException `json:"exception,omitempty"`
}

type FinalCounts struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Error int `json:"error"`
	Omit  int `json:"omit"`
	Todo  int `json:"todo"`
}

type FinalEvent struct {
	Type   string      `json:"type"`
	Time   float64     `json:"time"`
	Counts FinalCounts `json:"counts"`
}

type Decoder struct {
	Reader io.Reader
}

type DecodingCallbacks struct {
	OnEvent func(event interface{}) error
	OnSuite func(suite SuiteEvent) error
	OnTest  func(cases []CaseEvent, test TestEvent) error
	OnFinal func(suite SuiteEvent, final FinalEvent) error
}

func (self FinalEvent) Passed() bool {
	c := self.Counts
	return c.Total == c.Pass+c.Omit+c.Todo
}

func (self *SuiteEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *CaseEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *TestException) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *TestEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *FinalEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}

func (decoder Decoder) Decode(callbacks *DecodingCallbacks) (err error) {
	scanner := bufio.NewScanner(decoder.Reader)
	var currentSuite SuiteEvent
	var currentCases []CaseEvent
	currentCases = make([]CaseEvent, 0)

	for scanner.Scan() {
		if err != nil {
			continue
		}

		var event interface{}

		if err, event = UnmarshalEvent([]byte(scanner.Text())); err == nil {
			if callbacks.OnEvent != nil {
				callbacks.OnEvent(event)
			}

			switch event.(type) {
			case *SuiteEvent:
				se, _ := event.(*SuiteEvent)
				currentSuite = *se
				if callbacks.OnSuite != nil {
					err = callbacks.OnSuite(currentSuite)
					if err != nil {
						return
					}
				}
			case *CaseEvent:
				ce, _ := event.(*CaseEvent)
				numCases := len(currentCases)
				keepLevels := ce.Level - 1
				if numCases == 0 || keepLevels <= 0 {
					currentCases = []CaseEvent{*ce}
				} else {
					currentCases = append(currentCases[:keepLevels], *ce)
				}
			case *TestEvent:
				if callbacks.OnTest != nil {
					te, _ := event.(*TestEvent)
					err = callbacks.OnTest(currentCases, *te)
					if err != nil {
						return
					}
				}
			case *FinalEvent:
				if callbacks.OnFinal != nil {
					fe, _ := event.(*FinalEvent)
					err = callbacks.OnFinal(currentSuite, *fe)
					if err != nil {
						return
					}
				}
			}
		}
	}

	return
}

func TestLabel(cases []CaseEvent, test TestEvent) string {
	labelParts := []string{}
	for _, kase := range cases {
		labelParts = append(labelParts, kase.Label)
	}
	labelParts = append(labelParts, test.Label)
	return strings.Join(labelParts, " ▸ ")
}

func UnmarshalEvent(value []byte) (err error, event interface{}) {
	baseEvent := new(BaseEvent)
	if err = json.Unmarshal(value, baseEvent); err != nil {
		event = baseEvent
		fmt.Fprintln(os.Stderr, "Could not parse text:", string(value))
		return
	}

	switch baseEvent.Type {
	case "suite":
		event = new(SuiteEvent)
	case "case":
		event = new(CaseEvent)
	case "test":
		event = new(TestEvent)
	case "final":
		event = new(FinalEvent)
	}

	err = json.Unmarshal(value, &event)
	return
}
