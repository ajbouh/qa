package tapjio

//go:generate go-bindata -o $GOGENPATH/qa/tapjio/assets/bindata.go -pkg assets -prefix ../tapjio-assets/ ../tapjio-assets/...

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
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
	QaType *string `json:"qa:type,omitempty"`
}

type SuiteEvent struct {
	Type  string `json:"type"`
	Start string `json:"start"`
	Count int    `json:"count"`
	Seed  int    `json:"seed"`
	Rev   int    `json:"rev"`
}

func NewSuiteEvent(startTime time.Time, count int, seed int) *SuiteEvent {
	return &SuiteEvent {
		Type:  "suite",
		Start: startTime.Format("2006-01-02 15:04:05"),
		Count: count,
		Seed:  seed,
		Rev:   4,
	}
}

type CaseEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Label   string `json:"label"`
	Level   int    `json:"level"`
}

type TraceData struct {
	Name string           `json:"name"`
	Pid  interface{}      `json:"pid"`
	Tid  interface{}      `json:"tid"`
	Ph   string           `json:"ph"`
	Ts   float64          `json:"ts"`
	Dur  *float64         `json:"dur,omitempty"`
	Args *json.RawMessage `json:"args,omitempty"`
}

type TraceEvent struct {
	Type  string     `json:"type"`
	Data  *TraceData `json:"trace"`
}

type TestException struct {
	Message string `json:"message"`
	Class   string `json:"class"`

	// e.g. {
	//  "somerubyfile.rb": {
	//   "3":"class SkipTest < Minitest::Test",
	//   "4":"  def test_skip",
	//   "5":"    skip",
	//   "6":"  end",
	//   "7":"end"
	//  }
	// },
	Snippets map[string]map[string]string `json:"snippets"`

	// e.g. [
	//  "test/skip-test.rb:5"
	// ]
	Backtrace []BacktraceLocation `json:"backtrace"`
}

type BacktraceLocation struct {
	File      string            `json:"file"`
	Line      int               `json:"line"`
	Variables map[string]string `json:"variables"`
}

type TestStartedEvent struct {
	TapjType  string  `json:"type"`
	Type      string  `json:"qa:type"`
	Timestamp float64 `json:"qa:timestamp"`
	Label     string  `json:"qa:label"`
	Subtype   string  `json:"qa:subtype"`
	Filter    string  `json:"qa:filter"`

	Cases     []CaseEvent `json:"-"`
}

func newTestStartedEvent() *TestStartedEvent {
	return &TestStartedEvent{
		TapjType: "note",
	}
}

type TestEvent struct {
	Type      string  `json:"type"`
	Time      float64 `json:"time"`
	Label     string  `json:"label"`
	Subtype   string  `json:"subtype"`
	Status    Status  `json:"status"`
	Filter    string  `json:"filter,omitempty"`
	File      string  `json:"file,omitempty"`

	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`

	Cases []CaseEvent `json:"-"`

	Exception *TestException `json:"exception,omitempty"`
}

type ResultTally struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Error int `json:"error"`
	Omit  int `json:"omit"`
	Todo  int `json:"todo"`
}

func (r *ResultTally) Increment(status Status) {
	r.Total += 1

	switch status {
	case Pass:
		r.Pass += 1
	case Fail:
		r.Fail += 1
	case Error:
		r.Error += 1
	case Omit:
		r.Omit += 1
	case Todo:
		r.Todo += 1
	}
}

type FinalEvent struct {
	Type   string         `json:"type"`
	Time   float64        `json:"time"`
	Counts *ResultTally    `json:"counts"`
	Stats  map[string]int `json:"qa:stats,omitempty"`
	MetaStats map[string]int `json:"-"`

	Suite  *SuiteEvent `json:"-"`
}

func NewFinalEvent(suite *SuiteEvent) *FinalEvent {
	return &FinalEvent{
		Type:  "final", // TODO(adamb) Figure out how to make Type implied.
		Suite: suite,
		Stats: make(map[string]int),
		MetaStats: make(map[string]int),
		Counts: &ResultTally{},
	}
}

func (f *FinalEvent) IncrementStat(name string, amount int) {
	i, ok := f.Stats[name]
	if !ok {
		i = 0
	}
	f.Stats[name] = i + amount
}


type Decoder struct {
	reader io.Reader
}

type Visitor interface {
	TraceEvent(trace TraceEvent) error
	SuiteStarted(suite SuiteEvent) error
	TestStarted(test TestStartedEvent) error
	TestFinished(test TestEvent) error
	SuiteFinished(final FinalEvent) error
	End(reason error) error
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
		OnTestBegin: func(event TestStartedEvent) error {
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
		OnEnd: func(reason error) error {
			for _, visitor := range visitors {
				err := visitor.End(reason)
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
	OnTestBegin func(event TestStartedEvent) error
	OnTest  func(event TestEvent) error
	OnTrace func(event TraceEvent) error
	OnFinal func(event FinalEvent) error
	OnEnd func(reason error) error
}

func (s *DecodingCallbacks) SuiteStarted(event SuiteEvent) error {
	if s.OnSuite == nil {
		return nil
	}

	return s.OnSuite(event)
}
func (s *DecodingCallbacks) TestStarted(event TestStartedEvent) error {
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

func (s *DecodingCallbacks) End(reason error) error {
	if s.OnEnd == nil {
		return nil
	}

	return s.OnEnd(reason)
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

func incrementValue(p map[string]int, k string, increment int) {
	n, ok := p[k]
	if !ok {
		n = 0
	}
	p[k] = n + increment
}

func Decode(reader io.Reader, visitor Visitor) (err error) {
	decoder := json.NewDecoder(reader)

	var currentSuite *SuiteEvent
	var currentCases []CaseEvent
	currentCases = make([]CaseEvent, 0)

	byteCountsByEventType := make(map[string]int)
	countsByEventType := make(map[string]int)

	for {
		if err != nil {
			break
		}

		raw := &json.RawMessage{}
		err = decoder.Decode(raw)
		if err == io.EOF {
			err = nil
			break
		}

		if err != nil {
			break
		}

		b, _ := raw.MarshalJSON()
		var event interface{}
		if err, event = UnmarshalEvent(b); err != nil {
			break
		}

		switch event.(type) {
		case *SuiteEvent:
			se, _ := event.(*SuiteEvent)
			incrementValue(byteCountsByEventType, se.Type, len(b))
			incrementValue(countsByEventType, se.Type, 1)
			currentSuite = se
			err = visitor.SuiteStarted(*currentSuite)
		case *CaseEvent:
			ce, _ := event.(*CaseEvent)
			incrementValue(byteCountsByEventType, ce.Type, len(b))
			incrementValue(countsByEventType, ce.Type, 1)
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
			incrementValue(byteCountsByEventType, te.Type, len(b))
			incrementValue(countsByEventType, te.Type, 1)
			err = visitor.TraceEvent(*te)
		case *TestStartedEvent:
			tse, _ := event.(*TestStartedEvent)
			incrementValue(byteCountsByEventType, tse.Type, len(b))
			incrementValue(countsByEventType, tse.Type, 1)
			tse.Cases = currentCases
			err = visitor.TestStarted(*tse)
		case *TestEvent:
			te, _ := event.(*TestEvent)
			incrementValue(byteCountsByEventType, te.Type, len(b))
			incrementValue(countsByEventType, te.Type, 1)
			te.Cases = currentCases
			err = visitor.TestFinished(*te)
		case *FinalEvent:
			fe, _ := event.(*FinalEvent)
			incrementValue(byteCountsByEventType, fe.Type, len(b))
			incrementValue(countsByEventType, fe.Type, 1)
			fe.Suite = currentSuite
			for eventType, byteCount := range byteCountsByEventType {
				fe.MetaStats[eventType + "/bytes"] = byteCount
			}
			for eventType, count := range countsByEventType {
				fe.MetaStats[eventType + "/count"] = count
			}
			err = visitor.SuiteFinished(*fe)
		}
	}

	// NOTE(adamb) An error from a visitor may override the error that
	//     caused us to break out!
	maybeErr := visitor.End(err)
	if maybeErr != nil {
		err = maybeErr
	}

	return
}

func TestLabel(label string, cases []CaseEvent) string {
	labelParts := []string{}
	for _, kase := range cases {
		labelParts = append(labelParts, kase.Label)
	}
	labelParts = append(labelParts, label)
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
		event = NewFinalEvent(nil)
	case "note":
		if baseEvent.QaType != nil && *baseEvent.QaType == "test:begin" {
			event = newTestStartedEvent()
		}
	default:
		err = errors.New("Unknown type: '" + baseEvent.Type + "': " + string(value))
		return
	}

	err = json.Unmarshal(value, &event)
	return
}
