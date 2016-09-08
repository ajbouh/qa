package watch

import (
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"qa/fileevents"
	"qa/glob"
	"qa/run"
	"qa/runner"
	"qa/tapjio"
	"strings"
)

type Watch struct {
	consoleWriter io.Writer
	dir           string
	runnerConfig  runner.Config
	runEnv        *run.Env
	matchFns      [](func(path string) bool)

	// For file subscriptions.
	sub               *fileevents.Subscription
	pathsToWatch      *filePathCensus
	otherWatchedPaths []tapjio.FilePath
	ignoreDirs        []string

	depEntryIndex *testDependencyTable

	eventFilter *fileevents.EventContentChangeFilter
}

func RunnerConfigToWatchExpression(runnerConfig runner.Config, additionalAbsolutePaths []tapjio.FilePath) (string, interface{}, error) {
	dir, err := filepath.Abs(runnerConfig.FileLister.Dir())
	if err != nil {
		return "", nil, err
	}

	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", nil, err
	}

	expression := []interface{}{"anyof"}

	for _, pattern := range runnerConfig.FileLister.Patterns() {
		expandedPatterns, err := glob.ExpandGlobSubpatterns(pattern)
		if err != nil {
			return "", nil, err
		}

		for _, expandedPattern := range expandedPatterns {
			expression = append(expression, []string{"match", expandedPattern, "wholename"})
		}
	}

	for _, absolutePath := range additionalAbsolutePaths {
		subpath := absolutePath.RelativePathFrom(dir)
		if subpath == "" {
			continue
		}
		expression = append(expression, []string{"match", subpath, "wholename"})
	}

	return dir, map[string](interface{}){
		"expression": expression,
		"fields":     []string{"name", "new", "exists"},
		"defer_vcs":  true,
	}, nil
}

func NewWatch(consoleWriter io.Writer, dir string, runEnv *run.Env, sub *fileevents.Subscription, runnerConfig runner.Config) *Watch {
	matchFns := [](func(path string) bool){}
	for _, pattern := range runnerConfig.FileLister.Patterns() {
		fn, err := glob.ToMatchPathFn(filepath.Join(dir, pattern))
		if err != nil {
			panic(err)
		}
		matchFns = append(matchFns, fn)
	}

	// HACK(adamb) Shouldn't be hardcoding this here.
	ignoreDirs := []string{
		filepath.Join(dir, "bundle"),
		filepath.Join(dir, "tmp"),
	}

	return &Watch{
		consoleWriter: consoleWriter,
		dir:           dir,
		runnerConfig:  runnerConfig,
		runEnv:        runEnv,
		matchFns:      matchFns,
		sub:           sub,

		pathsToWatch:      newFilePathCensus(),
		otherWatchedPaths: []tapjio.FilePath{},
		depEntryIndex:     newTestDependencyTable(func(f tapjio.FilePath) bool { return shouldWatchPath(f, dir, ignoreDirs) }),
		ignoreDirs:        ignoreDirs,

		eventFilter: fileevents.NewEventContentChangeFilter(sha256.New),
	}
}

type staticFileLister struct {
	dir   string
	files []string
}

func (s staticFileLister) Dir() string {
	return s.dir
}

func (s staticFileLister) Patterns() []string {
	return s.files
}

func (s staticFileLister) ListFiles() ([]string, error) {
	return s.Patterns(), nil
}

func shouldWatchPath(path tapjio.FilePath, dir string, ignoreDirs []string) bool {
	if !path.IsParentDir(dir) {
		return false
	}

	for _, ignoreDir := range ignoreDirs {
		if path.IsParentDir(ignoreDir) {
			return false
		}
	}

	return true
}

func (m *Watch) shouldWatchPath(path tapjio.FilePath) bool {
	return shouldWatchPath(path, m.dir, m.ignoreDirs)
}

func (m *Watch) updateSubscription() error {
	// Recompute otherWatchedPaths.
	slice := m.pathsToWatch.ToSlice()
	otherWatchedPaths := make([]tapjio.FilePath, 0, len(slice))

	for _, fn := range m.matchFns {
		for _, path := range slice {
			if m.shouldWatchPath(path) && !path.Matches(fn) {
				// Keep paths that don't match glob and are within the dir.
				otherWatchedPaths = append(otherWatchedPaths, path)
			}
		}
	}

	previousOtherWatchedPaths := m.otherWatchedPaths
	if !filePathSlicesEq(previousOtherWatchedPaths, otherWatchedPaths) {
		_, expr, err := RunnerConfigToWatchExpression(m.runnerConfig, otherWatchedPaths)
		if err != nil {
			return err
		}

		err = m.sub.Update(expr)
		if err != nil {
			return err
		}

		m.otherWatchedPaths = otherWatchedPaths
	}

	return nil
}

func (m *Watch) visitor() tapjio.Visitor {
	updateSubscriptionsOnEnd := false

	var zeroDigest tapjio.FileDigest
	return &tapjio.DecodingCallbacks{
		OnTestFinish: func(event tapjio.TestFinishEvent) error {
			testFilePath := event.File.Expand(m.dir)

			deps := event.Dependencies
			if deps != nil {
				for i := deps.LoadedFileCount() - 1; i >= 0; i-- {
					loadedPath := deps.LoadedFilePath(i)
					if !m.shouldWatchPath(loadedPath) {
						continue
					}
					digest := deps.LoadedFileDigest(i)
					if digest != zeroDigest {
						m.eventFilter.SetDigest(loadedPath.String(), digest)
					}
				}

				// HACK(adamb) if testFilter doesn't end with ":0", then we know we should clear out existing error filters.
				if !strings.HasSuffix(event.Filter.String(), ":0") {
					m.depEntryIndex.RemoveFilter(tapjio.TestFilter(testFilePath.String() + ":0"))
				}

				entry := runner.TestDependencyEntry{
					Label:        tapjio.TestLabel(event.Label, event.Cases),
					File:         testFilePath,
					Filter:       event.Filter,
					Dependencies: *deps,
				}

				previous, _ := m.depEntryIndex.ReplaceEntry(entry)
				previousDeps := previous.Dependencies

				didDiscoverNewLoadedDeps, _ := m.pathsToWatch.Exchange(previousDeps.LoadedFilePaths(), deps.LoadedFilePaths())
				didDiscoverNewMissingDeps, _ := m.pathsToWatch.Exchange(previousDeps.Missing, deps.Missing)

				if didDiscoverNewLoadedDeps || didDiscoverNewMissingDeps {
					updateSubscriptionsOnEnd = true
				}
			}

			return nil
		},
		OnSuiteFinish: func(event tapjio.SuiteFinishEvent) error {
			if updateSubscriptionsOnEnd {
				err := m.updateSubscription()
				if err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func (m *Watch) matchesRunConfigPattern(filePath tapjio.FilePath) bool {
	for _, fn := range m.matchFns {
		if filePath.Matches(fn) {
			return true
		}
	}

	return false
}

func (m *Watch) makeRunEnv(rootDir string, testFiles []string, testFilters []tapjio.TestFilter) *run.Env {
	prunedRunnerConfig := new(runner.Config)
	*prunedRunnerConfig = m.runnerConfig
	prunedRunnerConfig.FileLister = staticFileLister{rootDir, testFiles}
	prunedRunnerConfig.Filters = testFilters
	if len(testFiles) > 1 {
		prunedRunnerConfig.SquashPolicy = runner.SquashByFile
	} else {
		prunedRunnerConfig.SquashPolicy = runner.SquashNothing
	}

	pruned := *m.runEnv
	pruned.RunnerConfigs = []runner.Config{
		*prunedRunnerConfig,
	}
	pruned.Visitor = tapjio.MultiVisitor([]tapjio.Visitor{
		pruned.Visitor,
		m.visitor(),
	})

	entriesByFile := map[tapjio.FilePath][]runner.TestDependencyEntry{}
	var zeroDigest tapjio.FileDigest
	pruned.TestRunnerVisitor = func(testRunner runner.TestRunner, lastRunner bool) error {
		for _, depEntry := range testRunner.Dependencies() {
			filePath := depEntry.File.Expand(m.dir)
			entries, _ := entriesByFile[filePath]
			entriesByFile[filePath] = append(entries, depEntry)

			deps := depEntry.Dependencies
			for i := deps.LoadedFileCount() - 1; i >= 0; i-- {
				loadedPath := deps.LoadedFilePath(i)
				if !m.shouldWatchPath(loadedPath) {
					continue
				}
				digest := deps.LoadedFileDigest(i)
				if digest != zeroDigest {
					m.eventFilter.SetDigest(loadedPath.String(), digest)
				}
			}
		}

		if lastRunner {
			// Only prime dependencies for test file path if we're running without filters. If we're running
			// with filters then we already primed and calling LimitFiltersForTestFilePath would be incorrect.
			if len(testFilters) == 0 {
				shouldUpdateSubscription := false
				for file, entries := range entriesByFile {
					addedEntries, removedEntries := m.depEntryIndex.PrimeAndPurgeFiltersForFilePath(file, entries)
					for _, entry := range removedEntries {
						deps := entry.Dependencies
						m.pathsToWatch.Exchange(deps.LoadedFilePaths(), []tapjio.FilePath{})
						m.pathsToWatch.Exchange(deps.Missing, []tapjio.FilePath{})
					}

					for _, entry := range addedEntries {
						deps := entry.Dependencies
						m.pathsToWatch.Exchange([]tapjio.FilePath{}, deps.LoadedFilePaths())
						m.pathsToWatch.Exchange([]tapjio.FilePath{}, deps.Missing)
					}

					if len(addedEntries)+len(removedEntries) > 0 {
						shouldUpdateSubscription = true
					}
				}

				if shouldUpdateSubscription {
					err := m.updateSubscription()
					if err != nil {
						return err
					}
				}
			}

			m.depEntryIndex.TrimCapacity()
		}

		return nil
	}

	return &pruned
}

func (m *Watch) WriteStatus() {
	patterns := ""
	for _, pattern := range m.runnerConfig.FileLister.Patterns() {
		if patterns == "" {
			patterns = pattern
		} else {
			patterns = patterns + ", " + pattern
		}
	}

	var otherFilesDesc string
	if len(m.otherWatchedPaths) > 0 {
		otherFilesDesc = fmt.Sprintf(" and %d other files", len(m.otherWatchedPaths))
	}
	fmt.Fprintf(m.consoleWriter, "\nWatching %s%s in %s...\n",
		patterns, otherFilesDesc, m.dir)
}

func (m *Watch) processSubscriptionEvent(runEnvChan chan *run.Env, fileevent *fileevents.Event) {
	// Some will match the runner config pattern. Others need to be looked up
	// in a table based on what we've learned.
	explicitTestFileSet := newFilePathSet()
	implicitTestFileSet := newFilePathSet()
	implicitTestEntries := []runner.TestDependencyEntry{}
	implicitTestFilters := newFilterSet()

	for _, changedFile := range fileevent.Files {
		changedFileName := changedFile.Name
		changedFilePath := tapjio.FilePath(filepath.Join(fileevent.Root, changedFileName))

		if m.matchesRunConfigPattern(changedFilePath) {
			if changedFile.Exists {
				fmt.Fprintf(m.consoleWriter, "⚡  %s\n", changedFileName)
				explicitTestFileSet.Add(changedFilePath)
			} else {
				m.depEntryIndex.RemoveFile(changedFilePath)
			}
			continue
		}

		if changedFile.Exists {
			if changedFile.New {
				fmt.Fprintf(m.consoleWriter, "🆕  %s\n", changedFileName)
			} else {
				fmt.Fprintf(m.consoleWriter, "✏️  %s\n", changedFileName)
			}
		} else {
			fmt.Fprintf(m.consoleWriter, "🗑  %s\n", changedFileName)
		}

		entries := m.depEntryIndex.AffectedEntries(changedFilePath)
		remainingLines := len(entries)
		connector := "├─"

		for _, entry := range entries {
			if remainingLines == 1 {
				connector = "└─"
			}

			fmt.Fprintf(m.consoleWriter, "   %s %s\n", connector, entry.Label)
			remainingLines--
		}

		for _, entry := range entries {
			testFile := entry.File
			testFilter := entry.Filter

			if explicitTestFileSet.Contains(testFile) {
				continue
			}

			// HACK(adamb) If we're supposed to run the whole file (because this was a *missing* dep), then
			//     don't try to re-run a specific filter.
			if strings.HasSuffix(testFilter.String(), ":0") {
				explicitTestFileSet.Add(testFile)
				continue
			}

			implicitTestEntries = append(implicitTestEntries, entry)
		}
	}

	// Since we might see things out of order, we need to check explicitTestFileSet before committing to implict tests
	for _, entry := range implicitTestEntries {
		if explicitTestFileSet.Contains(entry.File) {
			continue
		}

		implicitTestFileSet.Add(entry.File)
		implicitTestFilters.Add(entry.Filter)
	}

	if explicitTestFileSet.Len() > 0 || implicitTestFileSet.Len() > 0 {
		fmt.Fprintf(m.consoleWriter, "\n")
	}

	if explicitTestFileSet.Len() > 0 {
		runEnvChan <- m.makeRunEnv(fileevent.Root, explicitTestFileSet.StringSlice(), []tapjio.TestFilter{})
	}

	if implicitTestFileSet.Len() > 0 {
		runEnvChan <- m.makeRunEnv(fileevent.Root, implicitTestFileSet.StringSlice(), implicitTestFilters.Slice())
	}
}

func (m *Watch) ProcessSubscriptionEvents(runEnvChan chan *run.Env) {
	filteredEvents := m.eventFilter.FilterContentChanges(m.sub.Events)

	for fileevent := range filteredEvents {
		m.processSubscriptionEvent(runEnvChan, fileevent)
	}
}
