package watch

import (
	"fmt"
	"qa/tapjio"
)

type filePathCensus struct {
	counts map[tapjio.FilePath]int
}

func newFilePathCensus() *filePathCensus {
	return &filePathCensus{
		counts: make(map[tapjio.FilePath]int),
	}
}

func (p *filePathCensus) Increment(s tapjio.FilePath) int {
	// fmt.Fprintf(os.Stderr, "increment(\"%s\")\n", s)

	newCount := p.counts[s] + 1
	p.counts[s] = newCount
	return newCount
}

func (p *filePathCensus) Len() int {
	return len(p.counts)
}

func (p *filePathCensus) Delete(s tapjio.FilePath) {
	// fmt.Fprintf(os.Stderr, "delete(\"%s\")\n", s)
	delete(p.counts, s)
}

func (p *filePathCensus) Decrement(s tapjio.FilePath) int {
	// fmt.Fprintf(os.Stderr, "decrement(\"%s\")\n", s)

	count := p.counts[s]
	if count == 1 {
		delete(p.counts, s)
		return 0
	}

	if count < 1 {
		panic(fmt.Sprintf("Got count < 1 in map; p.counts[\"%s\"] %d", s, count))
	}

	newCount := count - 1
	p.counts[s] = newCount
	return newCount
}

func (p *filePathCensus) ToSlice() []tapjio.FilePath {
	slice := make([]tapjio.FilePath, len(p.counts))
	i := 0
	for s := range p.counts {
		slice[i] = s
		i++
	}

	return slice
}

// Exchange removes all previous entries and replaces them with current entries
// and returns false if it's a no-op.
func (p *filePathCensus) Exchange(previous, current []tapjio.FilePath) (bool, bool) {
	if filePathSlicesEq(previous, current) {
		return false, true
	}

	anyAdded := false
	for _, s := range current {
		if p.Increment(s) == 1 {
			anyAdded = true
		}
	}

	anyRemoved := false
	for _, s := range previous {
		if p.Decrement(s) == 0 {
			anyRemoved = true
		}
	}

	return anyRemoved || anyAdded, false
}
