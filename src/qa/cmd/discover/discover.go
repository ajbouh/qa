package discover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"qa/analysis"
	"qa/cmd"
	"qa/tapjio"
	"sync"
)

func outcomeDigest(m map[string]interface{}) (interface{}, error) {
	statusObj, ok := m["status"]
	if !ok {
		return nil, fmt.Errorf("No status found for event: %#v", m)
	}

	status, ok := statusObj.(string)
	if !ok {
		return nil, fmt.Errorf("Invalid status %#v found for event: %#v", status, m)
	}

	var e tapjio.TestException
	exception, ok := m["exception"]
	if ok {
		b, err := json.Marshal(exception)
		if err != nil {
			return nil, err
		}

		if err = json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
	}

	return tapjio.OutcomeDigestFor(tapjio.Status(status), &e)
}

func processDiscoveredEvents(decoder *json.Decoder, encoder *json.Encoder) error {
	var suite interface{}
	var caseLabels [](interface{})
	for {
		m := make(map[string]interface{})
		if err := decoder.Decode(&m); err == io.EOF {
			return nil
		} else if err != nil {
			log.Fatalf("error decoding event during discover: %#v", err)
		}

		switch m["type"] {
		case "suite":
			suite = m
			caseLabels = nil
		case "case":
			levelVal, ok := m["level"]
			var level int
			if ok {
				var levelF float64
				levelF, ok = levelVal.(float64)
				if ok {
					level = int(levelF)
				}
			}
			if !ok {
				level = 0
			}

			nextCaseLabels := make([](interface{}), level+1)
			if level < len(caseLabels) {
				copy(nextCaseLabels, caseLabels[0:level])
			} else {
				copy(nextCaseLabels, caseLabels)
			}
			nextCaseLabels[level] = m["label"]
			caseLabels = nextCaseLabels
		case "test":
			m["suite"] = suite
			m["case-labels"] = caseLabels
			var err error
			m["outcome-digest"], err = outcomeDigest(m)
			if err != nil {
				log.Printf("Error computing outcome-digest: %s", err)
			} else {
				if err := encoder.Encode(m); err != nil {
					log.Fatalf("error encoding event during discover: %#v", err)
				}
			}
		}
	}

	return nil
}

func Main(env *cmd.Env, argv []string) error {
	rd, wr := io.Pipe()
	defer rd.Close()

	decoder := json.NewDecoder(rd)
	encoder := json.NewEncoder(env.Stdout)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		processDiscoveredEvents(decoder, encoder)
	}()

	err := analysis.RunRuby(
		&cmd.Env{Stdin: bytes.NewBuffer([]byte{}), Stdout: wr, Stderr: env.Stderr},
		"tapj-discover.rb", argv[1:]...)
	wr.Close()

	wg.Wait()

	return err
}
