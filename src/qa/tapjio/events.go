package tapjio

//go:generate go-bindata -o $GOGENPATH/qa/tapjio/assets/bindata.go -pkg assets -prefix ../tapjio-assets/ ../tapjio-assets/...

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

type Status string

const (
	Begin Status = "begin"
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

type TraceEvent struct {
	Type  string           `json:"type"`
	Trace *json.RawMessage `json:"trace"`
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
	Type      string  `json:"type"`
	Timestamp float64 `json:"timestamp"`
	Time      float64 `json:"time"`
	Label     string  `json:"label"`
	Subtype   string  `json:"subtype"`
	Status    Status  `json:"status"`
	Filter    string  `json:"filter,omitempty"`
	File      string  `json:"file,omitempty"`

	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`

	Cases []CaseEvent

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

	Suite SuiteEvent
}

type Decoder struct {
	reader io.Reader
}

type Visitor interface {
	TraceEvent(trace TraceEvent) error
	SuiteStarted(suite SuiteEvent) error
	TestStarted(test TestEvent) error
	TestFinished(test TestEvent) error
	SuiteFinished(final FinalEvent) error
}

type multiVisitor struct {
	visitors []Visitor
}

func MultiVisitor(visitors []Visitor) Visitor {
	return &DecodingCallbacks{
		OnSuite: func(event SuiteEvent) error {
			for _, visitor := range visitors {
				err := visitor.SuiteStarted(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnTestBegin: func(event TestEvent) error {
			for _, visitor := range visitors {
				err := visitor.TestStarted(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnTest: func(event TestEvent) error {
			for _, visitor := range visitors {
				err := visitor.TestFinished(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnTrace: func(event TraceEvent) error {
			for _, visitor := range visitors {
				err := visitor.TraceEvent(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnFinal: func(event FinalEvent) error {
			for _, visitor := range visitors {
				err := visitor.SuiteFinished(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
}

type DecodingCallbacks struct {
	OnSuite func(event SuiteEvent) error
	OnTestBegin func(event TestEvent) error
	OnTest  func(event TestEvent) error
	OnTrace func(event TraceEvent) error
	OnFinal func(event FinalEvent) error
}

func (s *DecodingCallbacks) SuiteStarted(event SuiteEvent) error {
	if s.OnSuite == nil {
		return nil
	}

	return s.OnSuite(event)
}
func (s *DecodingCallbacks) TestStarted(event TestEvent) error {
	if s.OnTestBegin == nil {
		return nil
	}

	return s.OnTestBegin(event)
}
func (s *DecodingCallbacks) TestFinished(event TestEvent) error {
	if s.OnTest == nil {
		return nil
	}

	return s.OnTest(event)
}
func (s *DecodingCallbacks) TraceEvent(event TraceEvent) error {
	if s.OnTrace == nil {
		return nil
	}

	return s.OnTrace(event)
}
func (s *DecodingCallbacks) SuiteFinished(event FinalEvent) error {
	if s.OnFinal == nil {
		return nil
	}

	return s.OnFinal(event)
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
func (self *TraceEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *FinalEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}

func Decode(reader io.Reader, visitor Visitor) (err error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 30*1024*1024)
	var currentSuite SuiteEvent
	var currentCases []CaseEvent
	currentCases = make([]CaseEvent, 0)

	for scanner.Scan() {
		if err != nil {
			continue
		}

		var event interface{}

		text := scanner.Text()
		if err, event = UnmarshalEvent([]byte(text)); err == nil {
			switch event.(type) {
			case *SuiteEvent:
				se, _ := event.(*SuiteEvent)
				currentSuite = *se
				err = visitor.SuiteStarted(currentSuite)
			case *CaseEvent:
				ce, _ := event.(*CaseEvent)
				numCases := len(currentCases)
				keepLevels := ce.Level
				if numCases == 0 || keepLevels <= 0 {
					currentCases = []CaseEvent{*ce}
				} else if keepLevels > len(currentCases) {
					log.Fatal("Unexpected level for case event: ", ce.Level, " (was at level ", len(currentCases)-1, ")")
				} else {
					currentCases = append(currentCases[:keepLevels], *ce)
				}
			case *TraceEvent:
				te, _ := event.(*TraceEvent)
				err = visitor.TraceEvent(*te)
			case *TestEvent:
				te, _ := event.(*TestEvent)
				te.Cases = currentCases
				if te.Status == Begin {
					err = visitor.TestStarted(*te)
				} else {
					err = visitor.TestFinished(*te)
				}
			case *FinalEvent:
				fe, _ := event.(*FinalEvent)
				fe.Suite = currentSuite
				err = visitor.SuiteFinished(*fe)
			}

			if err != nil {
				return
			}
		}
	}

	return
}

func TestLabel(test TestEvent) string {
	labelParts := []string{}
	for _, kase := range test.Cases {
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
	case "trace":
		event = new(TraceEvent)
	case "final":
		event = new(FinalEvent)
	}

	err = json.Unmarshal(value, &event)
	return
}
