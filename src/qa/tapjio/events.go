package tapjio

//go:generate go-bindata -o $GOGENPATH/qa/tapjio/assets/bindata.go -pkg assets -prefix ../tapjio-assets/ ../tapjio-assets/...

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	Type   string  `json:"type"`
	QaType *string `json:"qa:type,omitempty"`
}

type SuiteBeginEvent struct {
	Type    string `json:"type"`
	Start   string `json:"start"`
	Count   int    `json:"count"`
	Seed    int    `json:"seed"`
	Rev     int    `json:"rev"`
	Label   string `json:"label,omitempty"`
	Coderef string `json:"coderef,omitempty"`
}

func NewSuiteBeginEvent(startTime time.Time, count int, seed int) *SuiteBeginEvent {
	return &SuiteBeginEvent{
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
	Type string     `json:"type"`
	Data *TraceData `json:"trace"`
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
	File       string            `json:"file"`
	Line       int               `json:"line"`
	Method     string            `json:"method"`
	BlockLevel int               `json:"block_level"`
	Variables  map[string]string `json:"variables"`
}

// TODO(adamb) This event name probably isn't so good.
type DependencyIndexEvent struct {
	TapjType   string     `json:"type"`
	Type       string     `json:"qa:type"`
	HexDigests []string   `json:"digests"`
	Files      []FilePath `json:"files"`
}

type TestBeginEvent struct {
	TapjType  string     `json:"type"`
	Type      string     `json:"qa:type"`
	Timestamp float64    `json:"qa:timestamp"`
	Label     string     `json:"qa:label"`
	Subtype   string     `json:"qa:subtype"`
	Filter    TestFilter `json:"qa:filter"`
	File      FilePath   `json:"qa:file"`

	Cases []CaseEvent `json:"-"`
}

func NewTestBeginEvent() *TestBeginEvent {
	return &TestBeginEvent{
		TapjType: "note",
		Type:     "test:begin",
	}
}

type TestFilter string

func (f TestFilter) String() string {
	return string(f)
}

func (f TestFilter) GreaterThanOrEqual(other TestFilter) bool {
	return string(f) >= string(other)
}

type FilePath string

func (t FilePath) IsAbs() bool {
	return path.IsAbs(string(t))
}

func (t FilePath) String() string {
	return string(t)
}

func (t FilePath) Expand(dir string) FilePath {
	if path.IsAbs(string(t)) {
		return t
	}

	return FilePath(filepath.Join(dir, string(t)))
}

func (t FilePath) IsParentDir(dir string) bool {
	s := string(t)
	dirLen := len(dir)

	return strings.HasPrefix(s, dir) && len(s) >= dirLen && s[dirLen] == '/'
}

func (t FilePath) MatchesPattern(r *regexp.Regexp) bool {
	return r.MatchString(string(t))
}

func (t FilePath) Matches(fn func(string) bool) bool {
	return fn(string(t))
}

func (t FilePath) RelativePathFrom(dir string) string {
	if !t.IsParentDir(dir) {
		return ""
	}

	return string(t)[len(dir)+1:]
}

type TestFinishEvent struct {
	Type    string     `json:"type"`
	Time    float64    `json:"time"`
	Label   string     `json:"label"`
	Subtype string     `json:"subtype"`
	Status  Status     `json:"status"`
	Filter  TestFilter `json:"filter,omitempty"`
	File    FilePath   `json:"file,omitempty"`
	Line    int        `json:"line"`

	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`

	Cases []CaseEvent `json:"-"`

	Dependencies *TestDependencies `json:"dependencies,omitempty"`
	Exception    *TestException    `json:"exception,omitempty"`
}

const DigestSize = 32

type FileDigest [DigestSize]byte

func FileDigestFromHash(hasher hash.Hash) FileDigest {
	if hasher.Size() != DigestSize {
		panic(fmt.Errorf("Can't return FileDigest from hasher. Expected Size() to be %d, got: %d", DigestSize, hasher.Size()))
	}

	var digest FileDigest
	for ix, b := range hasher.Sum(nil) {
		digest[ix] = b
	}

	return digest
}

type loadedDependencyDigestIndex struct {
	files   []FilePath
	digests []FileDigest
}

func (i *loadedDependencyDigestIndex) append(files []FilePath, hexDigests []string) {
	i.files = append(i.files, files...)
	newLen := len(i.digests) + len(hexDigests)
	if cap(i.digests) < newLen {
		newDigests := make([]FileDigest, len(i.digests), newLen)
		copy(newDigests, i.digests)
		i.digests = newDigests
	}

	newDigests := i.digests
	for hexDigestIx, hexDigest := range hexDigests {
		digestSlice, err := hex.DecodeString(hexDigest)
		var digest FileDigest
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not decode hex digest %d for %#v: %#v", hexDigestIx, files[hexDigestIx], hexDigest)
		} else {
			for ix, b := range digestSlice {
				digest[ix] = b
			}
		}

		newDigests = append(newDigests, digest)
	}
	i.digests = newDigests
}

func (i *loadedDependencyDigestIndex) filePath(ix int) FilePath {
	return i.files[ix]
}

func (i *loadedDependencyDigestIndex) digest(ix int) FileDigest {
	return i.digests[ix]
}

type TestDependencies struct {
	depIndex      *loadedDependencyDigestIndex
	LoadedIndices []int      `json:"loaded_indices"`
	Missing       []FilePath `json:"missing"`
}

func (t *TestDependencies) LoadedFileCount() int {
	return len(t.LoadedIndices)
}

func (t *TestDependencies) LoadedFilePaths() []FilePath {
	paths := make([]FilePath, len(t.LoadedIndices))
	depIndex := t.depIndex
	for i, index := range t.LoadedIndices {
		paths[i] = depIndex.filePath(index)
	}

	return paths
}

func (t *TestDependencies) LoadedFilePath(ix int) FilePath {
	return t.depIndex.filePath(t.LoadedIndices[ix])
}

func (t *TestDependencies) LoadedFileDigest(ix int) FileDigest {
	return t.depIndex.digest(t.LoadedIndices[ix])
}

type ResultTally struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Error int `json:"error"`
	Omit  int `json:"omit"`
	Todo  int `json:"todo"`
}

func (r *ResultTally) IncrementAll(other *ResultTally) {
	r.Total += other.Total
	r.Pass += other.Pass
	r.Fail += other.Fail
	r.Error += other.Error
	r.Omit += other.Omit
	r.Todo += other.Todo
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

type SuiteFinishEvent struct {
	Type      string         `json:"type"`
	Time      float64        `json:"time"`
	Counts    *ResultTally   `json:"counts"`
	Stats     map[string]int `json:"qa:stats,omitempty"`
	MetaStats map[string]int `json:"-"`

	Suite *SuiteBeginEvent `json:"-"`
}

func NewSuiteFinishEvent(suite *SuiteBeginEvent) *SuiteFinishEvent {
	return &SuiteFinishEvent{
		Type:      "final", // TODO(adamb) Figure out how to make Type implied.
		Suite:     suite,
		Stats:     make(map[string]int),
		MetaStats: make(map[string]int),
		Counts:    &ResultTally{},
	}
}

func (f *SuiteFinishEvent) IncrementStat(name string, amount int) {
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
	SuiteBegin(suite SuiteBeginEvent) error
	TestBegin(test TestBeginEvent) error
	TestFinish(test TestFinishEvent) error
	SuiteFinish(final SuiteFinishEvent) error
	End(reason error) error
}

func MultiVisitor(visitors []Visitor) Visitor {
	return &DecodingCallbacks{
		OnSuiteBegin: func(event SuiteBeginEvent) error {
			for _, visitor := range visitors {
				err := visitor.SuiteBegin(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnTestBegin: func(event TestBeginEvent) error {
			for _, visitor := range visitors {
				err := visitor.TestBegin(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnTestFinish: func(event TestFinishEvent) error {
			for _, visitor := range visitors {
				err := visitor.TestFinish(event)
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
		OnSuiteFinish: func(event SuiteFinishEvent) error {
			for _, visitor := range visitors {
				err := visitor.SuiteFinish(event)
				if err != nil {
					return err
				}
			}
			return nil
		},
		OnEnd: func(reason error) error {
			// Treat errors in OnEnd specially, so everyone gets a chance to clean up
			errors := []error{}
			for _, visitor := range visitors {
				err := visitor.End(reason)
				if err != nil {
					errors = append(errors, err)
				}
			}

			if len(errors) == 0 {
				return nil
			}

			if len(errors) == 1 {
				return errors[0]
			}

			return fmt.Errorf("Multiple errors encountered during tapjio.Visitor.End(%#v): %#v", reason, errors)
		},
	}
}

type DecodingCallbacks struct {
	OnSuiteBegin  func(event SuiteBeginEvent) error
	OnTestBegin   func(event TestBeginEvent) error
	OnTestFinish  func(event TestFinishEvent) error
	OnTrace       func(event TraceEvent) error
	OnSuiteFinish func(event SuiteFinishEvent) error
	OnEnd         func(reason error) error
}

func (s *DecodingCallbacks) SuiteBegin(event SuiteBeginEvent) error {
	if s.OnSuiteBegin == nil {
		return nil
	}

	return s.OnSuiteBegin(event)
}
func (s *DecodingCallbacks) TestBegin(event TestBeginEvent) error {
	if s.OnTestBegin == nil {
		return nil
	}

	return s.OnTestBegin(event)
}
func (s *DecodingCallbacks) TestFinish(event TestFinishEvent) error {
	if s.OnTestFinish == nil {
		return nil
	}

	return s.OnTestFinish(event)
}
func (s *DecodingCallbacks) TraceEvent(event TraceEvent) error {
	if s.OnTrace == nil {
		return nil
	}

	return s.OnTrace(event)
}
func (s *DecodingCallbacks) SuiteFinish(event SuiteFinishEvent) error {
	if s.OnSuiteFinish == nil {
		return nil
	}

	return s.OnSuiteFinish(event)
}

func (s *DecodingCallbacks) End(reason error) error {
	if s.OnEnd == nil {
		return nil
	}

	return s.OnEnd(reason)
}

func (self SuiteFinishEvent) Passed() bool {
	c := self.Counts
	return c.Total == c.Pass+c.Omit+c.Todo
}

func (self *SuiteBeginEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *CaseEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *TestException) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *TestFinishEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *TraceEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}
func (self *SuiteFinishEvent) String() string {
	return fmt.Sprintf("%#v", *self)
}

func incrementValue(p map[string]int, k string, increment int) {
	n, ok := p[k]
	if !ok {
		n = 0
	}
	p[k] = n + increment
}

func DecodeReader(reader io.Reader, visitor Visitor) error {
	return Decode(json.NewDecoder(reader), visitor)
}

func Decode(decoder *json.Decoder, visitor Visitor) (err error) {
	var currentSuite *SuiteBeginEvent
	var currentCases []CaseEvent
	currentCases = make([]CaseEvent, 0)

	byteCountsByEventType := make(map[string]int)
	countsByEventType := make(map[string]int)

	depIndex := &loadedDependencyDigestIndex{}

	var previousLoadedIndices []int

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
		case *SuiteBeginEvent:
			se, _ := event.(*SuiteBeginEvent)
			incrementValue(byteCountsByEventType, se.Type, len(b))
			incrementValue(countsByEventType, se.Type, 1)
			currentSuite = se
			err = visitor.SuiteBegin(*currentSuite)
			currentCases = nil
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
		case *DependencyIndexEvent:
			de, _ := event.(*DependencyIndexEvent)
			depIndex.append(de.Files, de.HexDigests)
		case *TraceEvent:
			te, _ := event.(*TraceEvent)
			incrementValue(byteCountsByEventType, te.Type, len(b))
			incrementValue(countsByEventType, te.Type, 1)
			err = visitor.TraceEvent(*te)
		case *TestBeginEvent:
			tse, _ := event.(*TestBeginEvent)
			incrementValue(byteCountsByEventType, tse.Type, len(b))
			incrementValue(countsByEventType, tse.Type, 1)
			tse.Cases = currentCases
			err = visitor.TestBegin(*tse)
		case *TestFinishEvent:
			te, _ := event.(*TestFinishEvent)
			incrementValue(byteCountsByEventType, te.Type, len(b))
			incrementValue(countsByEventType, te.Type, 1)
			te.Cases = currentCases
			if te.Dependencies != nil {
				te.Dependencies.depIndex = depIndex
				li := te.Dependencies.LoadedIndices
				reusePreviousLoadedIndices := false
				if len(li) == len(previousLoadedIndices) {
					reusePreviousLoadedIndices = true
					for ix, index := range li {
						if previousLoadedIndices[ix] != index {
							reusePreviousLoadedIndices = false
							break
						}
					}
				}

				if reusePreviousLoadedIndices {
					te.Dependencies.LoadedIndices = previousLoadedIndices
				} else if cap(li) != len(li) {
					shrunkLi := make([]int, len(li))
					copy(shrunkLi, li)
					te.Dependencies.LoadedIndices = shrunkLi
					previousLoadedIndices = te.Dependencies.LoadedIndices
				} else {
					previousLoadedIndices = te.Dependencies.LoadedIndices
				}
			}
			err = visitor.TestFinish(*te)
		case *SuiteFinishEvent:
			fe, _ := event.(*SuiteFinishEvent)
			incrementValue(byteCountsByEventType, fe.Type, len(b))
			incrementValue(countsByEventType, fe.Type, 1)
			fe.Suite = currentSuite
			for eventType, byteCount := range byteCountsByEventType {
				fe.MetaStats[eventType+"/bytes"] = byteCount
			}
			for eventType, count := range countsByEventType {
				fe.MetaStats[eventType+"/count"] = count
			}
			err = visitor.SuiteFinish(*fe)
		default:
			fmt.Fprintln(os.Stderr, "Unknown event", event)
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
		event = new(SuiteBeginEvent)
	case "case":
		event = new(CaseEvent)
	case "test":
		event = new(TestFinishEvent)
	case "trace":
		event = new(TraceEvent)
	case "final":
		event = NewSuiteFinishEvent(nil)
	case "note":
		if baseEvent.QaType == nil {
			return
		}

		switch *baseEvent.QaType {
		case "dependency":
			event = new(DependencyIndexEvent)
		case "test:begin":
			event = NewTestBeginEvent()
		default:
			return
		}
	default:
		err = errors.New("Unknown type: '" + baseEvent.Type + "': " + string(value))
		return
	}

	// fmt.Fprintf(os.Stderr, "Parse event, type %s with length: %d\n", baseEvent.Type, len(value))
	err = json.Unmarshal(value, &event)
	return
}
