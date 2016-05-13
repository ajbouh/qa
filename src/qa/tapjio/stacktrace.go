package tapjio

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"qa/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/google/pprof/driver"
	"github.com/google/pprof/profile"
)

// Extracts trace events from a tapj stream.

type stacktraceEmitter struct {
	writer io.Writer
}

func decodeFlamegraphSample(writer io.Writer, bytes []byte) error {
	traceMap := make(map[string](interface{}))
	err := json.Unmarshal(bytes, &traceMap)
	if err != nil {
		return err
	}

	x, ok := traceMap["args"]
	if !ok {
		return nil
	}

	xMap, ok := x.(map[string]interface{})
	if !ok {
		return nil
	}

	y, ok := xMap["data"]
	if !ok {
		return nil
	}

	ySlice, ok := y.([](interface{}))
	if !ok {
		return nil
	}

	for _, z := range ySlice {
		zSlice, ok := z.([](interface{}))
		if !ok {
			continue
		}
		backtrace := zSlice[0]
		count := zSlice[1]
		countN, _ := count.(float64)
		fmt.Fprintf(writer, "%s %d\n", backtrace, int(countN))
	}

	return nil
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

func decodeFlamegraphSampleV2(writer io.Writer, bytes []byte) error {
	profile := encodedProfile{}
	err := json.Unmarshal(bytes, &profile)
	if err != nil {
		return err
	}

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
		frameLength := samples[i]
		i++
		first := true
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
			if first {
				first = false
			} else {
				fmt.Fprintf(writer, ";")
			}
			fmt.Fprintf(writer, "%s", symbol)
		}
		fmt.Fprintf(writer, " %d\n", samples[i])
		i++
	}

	return nil
}

func decodePprof(writer io.Writer, jsonBytes []byte) error {
	traceMap := make(map[string](interface{}))
	err := json.Unmarshal(jsonBytes, &traceMap)
	if err != nil {
		return err
	}

	x, ok := traceMap["args"]
	if !ok {
		return nil
	}

	xMap, ok := x.(map[string]interface{})
	if !ok {
		return nil
	}

	y, ok := xMap["pprofGzBase64"]
	if !ok {
		return nil
	}

	yString, ok := y.(string)
	if !ok {
		return nil
	}

	profileGzBytes, err := base64.StdEncoding.DecodeString(yString)
	if err != nil {
		return err
	}

	z, ok := xMap["symbolsGzBase64"]
	if !ok {
		return nil
	}

	zString, ok := z.(string)
	if !ok {
		return nil
	}

	symbolsGzBytes, err := base64.StdEncoding.DecodeString(zString)
	if err != nil {
		return err
	}

	functionsByAddress := make(map[uint64]*profile.Function)
	gzipReader, err := gzip.NewReader(bytes.NewReader(symbolsGzBytes))
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(gzipReader)
	for scanner.Scan() {
		line := scanner.Text()
		split := strings.SplitN(line, ": ", 2)
		address, err := strconv.ParseUint(split[0], 16, 64)
		if err != nil {
			return err
		}
		name := split[1]
		functionsByAddress[address] = &profile.Function{ID: address, Name: name, SystemName: name}
	}

	var writeBuffer bytes.Buffer
	w := &pprofWriter{
		buffer:           &pprofWriteBuffer{&writeBuffer},
		outputBufferName: "y",
	}

	err = driver.PProf(&driver.Options{
		Writer: w,
		Fetch: &pprofFetcher{
			inputBufferSrcName: "x",
			gzBytes:            profileGzBytes,
		},
		Sym: &pprofSymbolizer{
			functionsByAddress: functionsByAddress,
		},
		Flagset: newPProfFlags(
			[]string{
				"-output=y", "-raw", "x",
			},
		),
	})
	if err != nil {
		return err
	}

	samples, err := pprof.ParseRaw(writeBuffer.Bytes())
	if err != nil {
		return err
	}

	for _, sample := range samples {
		fmt.Fprintf(writer, "%s %v\n", strings.Join(sample.Funcs, ";"), sample.Count)
	}

	return nil
}

type pprofSymbolizer struct {
	functionsByAddress map[uint64]*profile.Function
}

func (s *pprofSymbolizer) Symbolize(mode string, srcs driver.MappingSources, prof *profile.Profile) error {
	firstLocations := make(map[uint64]bool, len(prof.Sample))
	for _, s := range prof.Sample {
		firstLocations[s.Location[0].Address] = true
	}

	for _, l := range prof.Location {
		if b, _ := firstLocations[l.Address]; !b {
			l.Address++
		}

		fn, _ := s.functionsByAddress[l.Address]
		// if !ok {
		// 	name := "0x" + strconv.FormatUint(l.Address, 16)
		// 	fn = &profile.Function{ID: l.Address, Name: name, SystemName: name}
		// 	s.functionsByAddress[l.Address] = fn
		// }
		l.Line = []profile.Line{profile.Line{Function: fn}}
	}
	functions := make([]*profile.Function, len(s.functionsByAddress))
	i := 0
	for _, fn := range s.functionsByAddress {
		functions[i] = fn
		i++
	}
	prof.Function = functions
	return nil
}

// pprofFlags returns a flagset implementation based on the standard flag
// package from the Go distribution. It implements the plugin.FlagSet
// interface.

func newPProfFlags(args []string) *pprofFlags {
	return &pprofFlags{
		args: args,
		flag: flag.NewFlagSet("pprof", flag.ContinueOnError),
	}
}

type pprofFlags struct {
	args []string
	flag *flag.FlagSet
}

func (s *pprofFlags) Bool(o string, d bool, c string) *bool {
	return s.flag.Bool(o, d, c)
}

func (s *pprofFlags) Int(o string, d int, c string) *int {
	return s.flag.Int(o, d, c)
}

func (s *pprofFlags) Float64(o string, d float64, c string) *float64 {
	return s.flag.Float64(o, d, c)
}

func (s *pprofFlags) String(o, d, c string) *string {
	return s.flag.String(o, d, c)
}

func (s *pprofFlags) BoolVar(b *bool, o string, d bool, c string) {
	s.flag.BoolVar(b, o, d, c)
}

func (s *pprofFlags) IntVar(i *int, o string, d int, c string) {
	s.flag.IntVar(i, o, d, c)
}

func (s *pprofFlags) Float64Var(f *float64, o string, d float64, c string) {
	s.flag.Float64Var(f, o, d, c)
}

func (self *pprofFlags) StringVar(s *string, o, d, c string) {
	self.flag.StringVar(s, o, d, c)
}

func (s *pprofFlags) StringList(o, d, c string) *[]*string {
	return &[]*string{s.flag.String(o, d, c)}
}

func (s *pprofFlags) ExtraUsage() string {
	return ""
}

func (s pprofFlags) Parse(usage func()) []string {
	s.flag.Usage = usage
	s.flag.Parse(s.args)
	args := s.flag.Args()
	if len(args) == 0 {
		usage()
	}
	return args
}

type pprofWriteBuffer struct {
	buffer *bytes.Buffer
}

func (s *pprofWriteBuffer) Write(p []byte) (int, error) {
	return s.buffer.Write(p)
}

func (s *pprofWriteBuffer) Close() error {
	return nil
}

type pprofWriter struct {
	outputBufferName string
	buffer           *pprofWriteBuffer
}

func (s *pprofWriter) Open(name string) (io.WriteCloser, error) {
	if name == s.outputBufferName {
		return s.buffer, nil
	}

	return nil, errors.New("No support for opening: " + name)
}

type pprofFetcher struct {
	inputBufferSrcName string
	gzBytes            []byte
}

func (s *pprofFetcher) Fetch(src string, duration, timeout time.Duration) (*profile.Profile, string, error) {
	if s.inputBufferSrcName != src {
		return nil, "", errors.New("Unknown profile: " + src)
	}
	gzReader, err := gzip.NewReader(bytes.NewReader(s.gzBytes))
	if err != nil {
		return nil, "", err
	}
	prof, err := profile.Parse(gzReader)
	if err != nil {
		return nil, "", err
	}

	return prof, "", nil
}

func NewStacktraceEmitter(writer io.Writer) *stacktraceEmitter {
	return &stacktraceEmitter{
		writer: writer,
	}
}

func (t *stacktraceEmitter) TraceEvent(event TraceEvent) error {
	data := event.Data

	if data.Name == "flamegraph sample" {
		argsBytes, err := data.Args.MarshalJSON()
		if err != nil {
			return err
		}

		return decodeFlamegraphSample(t.writer, argsBytes)
	}

	if data.Name == "flamegraph sample v2" {
		argsBytes, err := data.Args.MarshalJSON()
		if err != nil {
			return err
		}

		return decodeFlamegraphSampleV2(t.writer, argsBytes)
	}

	if data.Name == "pprof data" {
		argsBytes, err := data.Args.MarshalJSON()
		if err != nil {
			return err
		}

		return decodePprof(t.writer, argsBytes)
	}

	return nil
}

func (t *stacktraceEmitter) TestStarted(event TestStartedEvent) error {
	return nil
}

func (t *stacktraceEmitter) SuiteFinished(event FinalEvent) error {
	return nil
}

func (t *stacktraceEmitter) SuiteStarted(event SuiteEvent) error {
	return nil
}

func (t *stacktraceEmitter) TestFinished(event TestEvent) error {
	return nil
}
