# QA Roadmap

## Basic functionality
- [ ] Write initial binary to detect what kind of test runner to use and use it
- [ ] Attaching a custom reporter to test runner
- [ ] Support for go, Java, Ruby, JavaScript, Python, PHP

## Parallelization
- [ ] TAP-J outputter to all test runners
- [ ] Use TAP-J reporter
- [ ] Parallelize test runs but still generate single TAP-J stream (and using the same reporter)

## Encapsulation
- [ ] Download and build implied dependencies for tests (e.g. language and test runner itself)
- [ ] Download and build explicit dependencies for tests (e.g. Gemfile)
- [ ] Support for using external execution environments (e.g. hermit, AWS lambda)

## Scaling
- [ ] Support for additional external execution environments, like Kubernetes, Mesos

## Tracking stats / artifacts
- [ ] Support for working with a local audit folder
- [ ] Support for working with a remote audit folder (e.g. S3)
- [ ] Generate trace file
- [ ] Generate single html file report with results, audits, flakiness statistics
- [ ] Add integration with system monitoring agents (e.g. performance copilot)

## Flakiness
- [ ] Fingerprint tests, augmenting TAP-J stream
- [ ] Fingerprint AUT (application under test), augmenting TAP-J stream
- [ ] Add TAP-J analysis tools, to detect rates of flakiness in tests
- [ ] Add support for marking some tests as (implicitly?) new, forcing them to be run many times and pass every time
- [ ] Add support for marking tests as flaky, separating their results from the results of other tests
- [ ] For tests that are failing (flaky or not), shows distribution of which line failed, test duration, version of code

## Continuous integration
- [ ] Add support for auto-filing issues (or updating existing issues) when a merged test fails that should not be flaky
- [ ] Suggests which flaky tests to debug first (based on heuristics)

## Local development
- [ ] Order test run during local development based on what's failed recently
- [ ] Line-level code coverage report
- [ ] Rerunning tests during local development affected by what code you just modified (test code or AUT, using code coverage analysis)
- [ ] Limit tests to files that are open in editor (open test files, open AUT files, etc)
- [ ] Can run with git-bisect to search for commit that introduced a bug
- [ ] Suggest which failing tests to debug first (based on heuristics)

## Correctness
- [ ] Add support to run tests in OS-specific sandbox for OS X
- [ ] Add support for overriding network syscalls (e.g. DNS, TCP connections)
- [ ] Add support for overriding w/ libfaketime
- [ ] Provide a way to exactly reproduce failures (e.g. with Mozilla's rr)
