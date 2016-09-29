package tapjio

import (
	"encoding/json"
	"io"
	"log"
)

// Extracts trace events from a tapj stream.

type TraceWriter struct {
	writer  io.WriteCloser
	encoder *json.Encoder
	delim   string
}

func NewTraceWriter(writer io.WriteCloser) *TraceWriter {
	return &TraceWriter{
		writer:  writer,
		encoder: json.NewEncoder(writer),
		delim:   "[\n",
	}
}

func (t *TraceWriter) TraceEvent(event TraceEvent) error {
	_, err := io.WriteString(t.writer, t.delim)
	if err != nil {
		log.Fatal("Could not write delimiter", err)
		return err
	}

	err = t.encoder.Encode(event.Data)
	if err != nil {
		log.Fatal("Could not encode event", err, event.Data)
		return err
	}

	t.delim = ","

	return nil
}

func (t *TraceWriter) AwaitAttach(event AwaitAttachEvent) error {
	return nil
}

func (t *TraceWriter) SuiteFinish(event SuiteFinishEvent) error {
	return nil
}

func (t *TraceWriter) SuiteBegin(event SuiteBeginEvent) error {
	return nil
}

func (t *TraceWriter) TestBegin(event TestBeginEvent) error {
	return nil
}

func (t *TraceWriter) TestFinish(event TestFinishEvent) error {
	return nil
}

func (t *TraceWriter) End(reason error) error {
	return t.writer.Close()
}
