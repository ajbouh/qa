package tapjio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/golang-lru"
)

type stacktraceWeightCache struct {
	cache *lru.Cache
}

func newStacktraceWeightCache(size int, onEvict func(stacktraceKey string, weight int)) *stacktraceWeightCache {
	cache, _ := lru.NewWithEvict(size, func(key interface{}, value interface{}) {
		keyString, valueIntPtr := castKeyValue(key, value)
		onEvict(keyString, *valueIntPtr)
	})

	return &stacktraceWeightCache{cache}
}

func castKeyValue(key interface{}, value interface{}) (string, *int) {
	keyString, _ := key.(string)
	weightPtr, _ := value.(*int)
	return keyString, weightPtr
}

func (s *stacktraceWeightCache) Purge() {
	s.cache.Purge()
}

func (s *stacktraceWeightCache) Add(key string, weight int) {
	value, ok := s.cache.Get(key)
	if ok {
		_, weightPtr := castKeyValue(key, value)
		*weightPtr = *weightPtr + weight
	} else {
		w := weight
		s.cache.Add(key, &w)
	}
}

type stacktraceWriter struct {
	writer    io.Writer
	lastError error
	cache     *stacktraceWeightCache
}

func newStacktraceWriter(writer io.Writer) *stacktraceWriter {
	var sw *stacktraceWriter
	evict := func(key string, weight int) {
		_, err := fmt.Fprintf(sw.writer, "%s %d\n", key, weight)
		if sw.lastError != nil {
			sw.lastError = err
		}
	}
	sw = &stacktraceWriter{
		writer: writer,
		cache:  newStacktraceWeightCache(16, evict),
	}
	return sw
}

func (s *stacktraceWriter) checkAndClearLastError() error {
	if s.lastError != nil {
		err := s.lastError
		s.lastError = nil
		return err
	}

	return nil
}

func (s *stacktraceWriter) Finish() error {
	s.cache.Purge()
	return s.checkAndClearLastError()
}

func (s *stacktraceWriter) EmitStacktrace(key string, weight int) error {
	s.cache.Add(key, weight)
	return s.checkAndClearLastError()
}

// Extracts trace events from a tapj stream.

type stacktraceEmitter struct {
	writer io.Writer
	closer io.Closer
}

type encodedProfile struct {
	Samples        []int    `json:"samples"`
	SymbolPrefixes []int    `json:"symbolPrefixes"`
	BaseSymbols    []string `json:"symbols"`
}

// TODO(adamb) Properly unmarshal bytes in other two decoders.
//     In complex decoder, unmarshal bytes to encodedProfile struct,
//     then build proper symbol table (possibly lazily) from encodedProfile.
//     Use built symbol table to emit stacktrace writer.
//     Use msgpack (if available in ruby subprocess!) to shrink data size even further?
//     Consider defining TAP-MSGPACK (which is just TAP-J converted to msgpack)

func decodeFlamegraphSample(writer io.Writer, b []byte) error {
	profile := encodedProfile{}
	err := json.Unmarshal(b, &profile)
	if err != nil {
		return err
	}

	se := newStacktraceWriter(writer)

	symbolPrefixes := profile.SymbolPrefixes
	prev := ""
	useSymbols := make([]string, len(profile.BaseSymbols))
	for ix, baseSymbol := range profile.BaseSymbols {
		prefixLen := symbolPrefixes[ix]
		var symbol string
		if prefixLen > 0 {
			symbol = prev[0:prefixLen] + baseSymbol
		} else {
			symbol = baseSymbol
		}
		useSymbols[ix] = symbol
		prev = symbol
	}

	i := 0
	samples := profile.Samples
	for i < len(samples) {
		var buffer bytes.Buffer
		frameLength := samples[i]
		i++
		previousSymbol := ""
		for frameLength > 0 {
			sample := samples[i]
			i++
			frameLength--
			symbol := useSymbols[sample]
			if strings.HasPrefix(symbol, "block ") {
				continue
			}
			if strings.HasPrefix(symbol, "RSpec::") {
				symbol = "RSpec::..."
			}
			if symbol == "RSpec::..." && previousSymbol == symbol {
				continue
			}

			previousSymbol = symbol
			if buffer.Len() > 0 {
				buffer.WriteByte(';')
			}
			buffer.Write([]byte(symbol))
		}
		se.EmitStacktrace(buffer.String(), samples[i])
		i++
	}

	if err := se.Finish(); err != nil {
		return err
	}

	return nil
}

func NewStacktraceEmitCloser(writer io.WriteCloser) *stacktraceEmitter {
	return &stacktraceEmitter{
		writer: writer,
		closer: writer,
	}
}

func NewStacktraceEmitter(writer io.Writer) *stacktraceEmitter {
	return &stacktraceEmitter{
		writer: writer,
	}
}

func (t *stacktraceEmitter) TraceEvent(event TraceEvent) error {
	data := event.Data

	if data.Name == "flamegraph sample v2" {
		argsBytes, err := data.Args.MarshalJSON()
		if err != nil {
			return err
		}

		return decodeFlamegraphSample(t.writer, argsBytes)
	}

	return nil
}

func (t *stacktraceEmitter) TestBegin(event TestBeginEvent) error {
	return nil
}

func (t *stacktraceEmitter) SuiteFinish(event SuiteFinishEvent) error {
	return nil
}

func (t *stacktraceEmitter) SuiteBegin(event SuiteBeginEvent) error {
	return nil
}

func (t *stacktraceEmitter) TestFinish(event TestFinishEvent) error {
	return nil
}

func (t *stacktraceEmitter) End(reason error) error {
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}
