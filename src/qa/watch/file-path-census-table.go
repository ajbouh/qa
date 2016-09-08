package watch

import (
	"qa/tapjio"
)

type filePathCensusTable map[tapjio.FilePath]*filePathCensus

func newFilePathCensusTable() filePathCensusTable {
	return make(filePathCensusTable)
}

func (s filePathCensusTable) Slice(key tapjio.FilePath) []tapjio.FilePath {
	census, present := s[key]
	if !present {
		return []tapjio.FilePath{}
	}

	return census.ToSlice()
}

func (s filePathCensusTable) Remove(key, value tapjio.FilePath) {
	counts, present := s[key]
	if present {
		counts.Delete(value)
		if counts.Len() == 0 {
			delete(s, key)
		}
	}
}

func (s filePathCensusTable) Increment(key, value tapjio.FilePath) {
	counts, present := s[key]
	if !present {
		counts = newFilePathCensus()
		s[key] = counts
	}

	counts.Increment(value)
}

func (s filePathCensusTable) Decrement(key, value tapjio.FilePath) {
	counts, present := s[key]
	if present {
		counts.Decrement(value)
		if counts.Len() == 0 {
			delete(s, key)
		}
	}
}
