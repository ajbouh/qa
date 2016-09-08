package watch

import (
	"qa/tapjio"
)

type filterTable map[tapjio.FilePath]*filterSet

func newFilterTable() filterTable {
	return make(filterTable)
}

func (s filterTable) TrimCapacity() {
	for _, set := range s {
		set.TrimCapacity()
	}
}

func (s filterTable) Add(key tapjio.FilePath, value tapjio.TestFilter) {
	set, present := s[key]
	if !present {
		set = newFilterSet()
		s[key] = set
	}

	set.Add(value)
}

func (s filterTable) Slice(key tapjio.FilePath) []tapjio.TestFilter {
	set, present := s[key]
	if !present {
		return []tapjio.TestFilter{}
	}

	return set.Slice()
}

func (s filterTable) RemoveAll(key tapjio.FilePath) {
	delete(s, key)
}

func (s filterTable) Remove(key tapjio.FilePath, value tapjio.TestFilter) {
	set, present := s[key]
	if present {
		set.Remove(value)
		if set.Len() == 0 {
			delete(s, key)
		}
	}
}
