package watch

import (
	"qa/runner"
	"qa/tapjio"
	"sync"
)

type testDependencyTable struct {
	shouldWatch func(tapjio.FilePath) bool

	byFilter map[tapjio.TestFilter]runner.TestDependencyEntry

	// For bookkeeping when test files are modified or removed.
	filtersByTestFilePath map[tapjio.FilePath][]tapjio.TestFilter

	// For triggering tests when non-test files change.
	filtersByDepFilePath filterTable

	mutex *sync.Mutex
}

func newTestDependencyTable(shouldWatch func(tapjio.FilePath) bool) *testDependencyTable {
	return &testDependencyTable{
		byFilter:    make(map[tapjio.TestFilter]runner.TestDependencyEntry),
		shouldWatch: shouldWatch,

		filtersByDepFilePath:  newFilterTable(),
		filtersByTestFilePath: make(map[tapjio.FilePath][]tapjio.TestFilter),

		mutex: &sync.Mutex{},
	}
}

func (t *testDependencyTable) TrimCapacity() {
	t.filtersByDepFilePath.TrimCapacity()
}

func (t *testDependencyTable) PrimeAndPurgeFiltersForFilePath(testFilePath tapjio.FilePath, entries []runner.TestDependencyEntry) ([]runner.TestDependencyEntry, []runner.TestDependencyEntry) {
	mutex := t.mutex
	mutex.Lock()
	defer mutex.Unlock()

	filters := make([]tapjio.TestFilter, len(entries))
	added := []runner.TestDependencyEntry{}
	removed := []runner.TestDependencyEntry{}

	for ix, entry := range entries {
		filter := entry.Filter
		filters[ix] = filter

		_, wasPresent := t.byFilter[filter]
		if wasPresent {
			continue
		}

		added = append(added, entry)
		t.byFilter[filter] = entry

		deps := entry.Dependencies
		for i := deps.LoadedFileCount() - 1; i >= 0; i-- {
			depFilePath := deps.LoadedFilePath(i)
			if !t.shouldWatch(depFilePath) {
				continue
			}
			t.filtersByDepFilePath.Add(depFilePath, filter)
		}
		for _, depFilePath := range deps.Missing {
			if !t.shouldWatch(depFilePath) {
				continue
			}
			t.filtersByDepFilePath.Add(depFilePath, filter)
		}
	}

	prevFilters, _ := t.filtersByTestFilePath[testFilePath]
	t.filtersByTestFilePath[testFilePath] = filters

	if len(prevFilters) > 0 {
		currentFilterSet := newFilterSet()
		for _, filter := range filters {
			currentFilterSet.Add(filter)
		}
		for _, filter := range prevFilters {
			if currentFilterSet.Contains(filter) {
				continue
			}
			entry, wasPresent := t.removeFilter(filter)
			if wasPresent {
				removed = append(removed, entry)
			}
		}
	}

	return added, removed
}

func (t *testDependencyTable) AffectedEntries(changedPath tapjio.FilePath) []runner.TestDependencyEntry {
	mutex := t.mutex
	mutex.Lock()
	defer mutex.Unlock()

	slice := t.filtersByDepFilePath.Slice(changedPath)
	entries := make([]runner.TestDependencyEntry, 0, len(slice))
	for _, filter := range slice {
		entry, present := t.byFilter[filter]
		if !present {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

func (t *testDependencyTable) RemoveFile(file tapjio.FilePath) {
	mutex := t.mutex
	mutex.Lock()
	defer mutex.Unlock()

	filters, ok := t.filtersByTestFilePath[file]
	if !ok {
		return
	}
	delete(t.filtersByTestFilePath, file)

	for _, filter := range filters {
		t.removeFilter(filter)
	}
}

func (t *testDependencyTable) RemoveFilter(filter tapjio.TestFilter) (runner.TestDependencyEntry, bool) {
	mutex := t.mutex
	mutex.Lock()
	defer mutex.Unlock()

	return t.removeFilter(filter)
}

func (t *testDependencyTable) ReplaceEntry(entry runner.TestDependencyEntry) (runner.TestDependencyEntry, bool) {
	mutex := t.mutex
	mutex.Lock()
	defer mutex.Unlock()

	filter := entry.Filter
	prev, wasPresent := t.byFilter[filter]
	t.byFilter[filter] = entry

	removedDepFiles := newFilePathSet()

	prevDeps := prev.Dependencies
	for i := prevDeps.LoadedFileCount() - 1; i >= 0; i-- {
		depFilePath := prevDeps.LoadedFilePath(i)
		if !t.shouldWatch(depFilePath) {
			continue
		}
		removedDepFiles.Add(depFilePath)
	}
	for _, depFilePath := range prevDeps.Missing {
		if !t.shouldWatch(depFilePath) {
			continue
		}
		removedDepFiles.Add(depFilePath)
	}

	deps := entry.Dependencies
	for i := deps.LoadedFileCount() - 1; i >= 0; i-- {
		depFilePath := deps.LoadedFilePath(i)
		if !t.shouldWatch(depFilePath) {
			continue
		}
		if removedDepFiles.Remove(depFilePath) {
			continue
		}
		t.filtersByDepFilePath.Add(depFilePath, filter)
	}
	for _, depFilePath := range deps.Missing {
		if !t.shouldWatch(depFilePath) {
			continue
		}
		if removedDepFiles.Remove(depFilePath) {
			continue
		}
		t.filtersByDepFilePath.Add(depFilePath, filter)
	}

	for _, depFilePath := range removedDepFiles.Slice() {
		t.filtersByDepFilePath.Remove(depFilePath, filter)
	}

	return prev, wasPresent
}

func (t *testDependencyTable) removeFilter(filter tapjio.TestFilter) (runner.TestDependencyEntry, bool) {
	prev, wasPresent := t.byFilter[filter]
	delete(t.byFilter, filter)

	if !wasPresent {
		return prev, wasPresent
	}

	prevDeps := prev.Dependencies
	for i := prevDeps.LoadedFileCount() - 1; i >= 0; i-- {
		loadedFile := prevDeps.LoadedFilePath(i)
		if !t.shouldWatch(loadedFile) {
			continue
		}
		t.filtersByDepFilePath.Remove(loadedFile, filter)
	}

	for _, missingFile := range prevDeps.Missing {
		if !t.shouldWatch(missingFile) {
			continue
		}
		t.filtersByDepFilePath.Remove(missingFile, filter)
	}

	return prev, wasPresent
}
