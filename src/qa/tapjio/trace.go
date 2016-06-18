package tapjio

import (
	"encoding/json"
	"io"
	"log"
)

// Extracts trace events from a tapj stream.

type TraceWriter struct {
	writer  io.Writer
	encoder *json.Encoder
	delim   string
}

func NewTraceWriter(writer io.Writer) *TraceWriter {
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

func (t *TraceWriter) SuiteFinished(event FinalEvent) error {
	return nil
}

func (t *TraceWriter) SuiteStarted(event SuiteEvent) error {
	return nil
}

func (t *TraceWriter) TestStarted(event TestStartedEvent) error {
	return nil
}

func (t *TraceWriter) TestFinished(event TestEvent) error {
	return nil
}

func (t *TraceWriter) End(reason error) error {
	return nil
}
