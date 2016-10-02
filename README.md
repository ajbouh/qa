# QA: The last (Ruby) test runner you'll ever need

QA is a lightweight tool for running your tests *fast*.

[![qa minitest asciicast](https://asciinema.org/a/45lb9tdc9lk22jkp4nstht1g0.png)](https://asciinema.org/a/45lb9tdc9lk22jkp4nstht1g0)

Advances in type systems, compiler technology, and prototyping environments (to name a few) have helped make many software engineering activities more productive. QA is an effort to make similar strides for automated testing tools.

## What can QA help me do today?

1. Run your tests faster. Run `qa rspec`, `qa minitest`, or `qa test-unit` in your project directory and watch your test results scream by as they run in parallel. QA provides a beautiful, easy to understand report. No Rakefile necessary!

2. See which tests are slowing you down. QA highlights tests that are dramatically slower than average. Look for the 🐌 at the end of successful testrun!

3. See per-test stderr and stdout. Even when running tests in parallel!

4. Investigate test performance by generating flamegraphs (or icicle graph) for the entire testrun. See `-save-flamegraph`, `-save-icegraph`, and `-save-palette`.

5. Run your tests in parallel. QA does this for you automatically. Use `-squash=none` to run each *method* in a separate child process. The default is `-squash=file`, which runs each *file* in its own process.

6. Analyze and eliminate [test flakiness](#whatis_flaky). The `-archive` option records test outcomes across different runs. Use the `qa flaky` command with the same `-archive` option to identify and diagnose flaky tests. This is new functionality, so please [open an issue](https://github.com/ajbouh/qa/issues/new) with questions and feedback!

7. Track threads, GC, require, SQL queries, and other noteworthy operations in a tracing format that can be used with the `chrome://tracing` tool, using `-save-trace` option.

8. See source code snippets and actual values of local variables for each frame of an error's stack trace.

9. Record test output as TAP-J, using `-save-tapj` option.

10. Automatically partition Rails tests across multiple databases, one per worker (Using custom ActiveRecord integration logic). If the required test databases do not exist, they will be setup automatically before tests begin. NOTE This functionality is highly experimental. Disable it with `-warmup=false`. Please [open an issue](https://github.com/ajbouh/qa/issues/new) if you have trouble.

## What languages and test frameworks does QA support?

Ruby 2.3+, and any of: RSpec, MiniTest, test-unit.

Be sure to use `bundle exec` when you run qa, if you're managing dependencies with Bundler. For example, if you're using Rspec:
```
bundle exec qa rspec
```

## What will QA help me do tomorrow?

1. A faster and more focused development cycle. QA will prioritize test ordering based on the files you're changing and what's failed recently.

2. Run your tests *even faster*. QA will package your test execution environment, run it on a massive fleet of remote machines (like [AWS Lambda](https://aws.amazon.com/lambda/)) and stream the results back to your terminal in real time.

3. Support more languages and testing frameworks. Please [open an issue](https://github.com/ajbouh/qa/issues/new) with your request!

## Getting started with QA

Starting with the 0.18 release, QA is now *only* available as the [qa-tool gem](https://rubygems.org/gems/qa-tool). You can add 'qa-tool' to your gem's development dependencies, or add the following to your Gemfile:
```
gem 'qa-tool'
```

Don't forget to run `bundle install` after editing your Gemfile!

See below for an example usage.

## How to use QA to run your tests

For directory structure that looks like:

```
lib/
  foo.rb
test/
  test-foo.rb
```

Example usage and output:
```
> cd $project
> qa minitest
...
```

## Troubleshooting QA

Since QA is still in alpha, there are a number of rough edges.

If `qa` seems to be acting strangely and isn't providing a reasonable error message, you may be experiencing a bug relating to swallowed error output. This is tied to QA's stdout and stderr capture logic. Adding the `-capture-standard-fds=false` option will disable the capture logic and should allow the original error to bubble up. Please [open an issue](https://github.com/ajbouh/qa/issues/new) with the error output.

## What are flaky tests?<a name="whatis_flaky"></a>

For a fast moving software team, automated tests are the last line of defense against shipping broken software to customers. But there's a dirty little secret in the world of automated testing: flaky tests. A flaky test fails intermittently with some (apparently random) probability. A flaky test doesn't care that it just passed 9 times in a row. On the 10th run, it will relish the chance to slowly drain your team's sanity.

Flaky tests sap your confidence in the rest of your tests. Their existence robs you of the peace of mind from seeing a test's "PASS" status. Every team has to battle flaky tests at some point, and few succeed in keeping them at bay. In fact, flakiness may wear you down to the point where you re-run a failed test, hoping that "MAYBE it is _just flaky_". Or worse, you comment it out, and suffer a customer-facing bug that would have been covered by the test!

So that's the bad news: by their very nature, flaky tests are hard to avoid. In many cases they start out looking like healthy tests. But when running on a machine under heavy load, they rear their ugly, randomly failing heads.  In some cases they may only fail when the network is saturated. (Which is a reason to avoid tests that rely on third party services in the first place.) Or the opposite could happen: you upgrade a dependency or language runtime to a faster version, and this speeds up the testrun enough to unveil latent flakiness you never recognized. Such are the perverse economics of flaky tests.

## How do I use QA to detect flaky tests?

To analyze the last few days worth of test results, you can use the `qa flaky` command. It's important to use the same value for `QA_ARCHIVE` (or `-archive`) as given to other `qa` commands. For example, continuing the session from above:

[![qa flaky asciicast](https://asciinema.org/a/dhdetw07drgyz78yr66bm57va.png)](https://asciinema.org/a/dhdetw07drgyz78yr66bm57va)


## How does QA detect flaky tests?

At a high level, QA considers a test to be flaky if, for a particular code revision, that test has both passed and failed. That's why you should provide a `-suite-coderef` value to `qa` commands.

At a low level, QA uses a few tricks to find as many examples of a flaky failure as it can. The actual algorithm for discovering flaky tests is:
- Fingerprint all failures using:
  - class of the failure (e.g. Exception, AssertionFailed, etc.)
  - line of source code that generated the failure (but not line number)
  - method and file names present in the stack trace (but not line numbers)
- Find all tests that, for a single revision, have both passed and failed.
- Put test failures from different revisions in the same bucket if their fingerprint matches a known flaky test

## How will future versions of QA help me with test flakiness?
With QA, we've set out to address the shortcomings we see with today's testing tools. We want a toolset that's *fast* and gives us more firepower for dealing with the reality of flaky tests.

- **Testing code that includes dependencies you didn't write?** QA will isolate tests from network services using an OS-specific sandbox.

- **Want to avoid merging new tests that could be flaky?** QA would run new tests repeatedly to vet them for flakiness first. QA would also run tests with less CPU and I/O than normal to stress their assumptions. Merge with confidence if things look good, or debug with confidence using QA.

- **Debugging a flaky test?** QA will help you understand the rate of flakiness and help you to reproduce it. QA will make it easy to discover attributes relating to the failure and to search data across previous test failures in the wild.

- **Think you've fixed a flaky test?** Know the number of rebuilds needed to be confident you've fixed it.

- **Already merge a flaky test?** QA will integrate with your [issue tracker], so you can easily submit a new issue for a flaky test. Once the issue is assigned, QA can ignore the outcomes of those flaky tests to protect the integrity of your other (non-flaky) tests.

## QA Roadmap

### Basic functionality
- [x] Attaching a custom reporter to test runner
- [x] Use process forking to amortize cost of starting test runner
- [ ] Support for Go, Java, JavaScript, Python, PHP

### Parallelization
- [x] TAP-J outputter to all test runners
- [x] Use TAP-J reporter
- [x] Parallelize test runs but still generate single TAP-J stream (and using the same reporter)

### Scaling
- [ ] Support for using external execution environments (e.g. hermit, AWS lambda)
- [ ] Support for additional external execution environments, like Kubernetes, Mesos

### Tracking stats / artifacts
- [x] Support for working with a local audit folder
- [ ] Support for working with a remote audit folder (e.g. S3)
- [x] Generate trace file
- [ ] Generate single html file report with results, audits, flakiness statistics
- [ ] Add integration with system monitoring agents (e.g. performance copilot)

### Flakiness
- [X] Fingerprint tests, augmenting TAP-J stream
- [ ] Fingerprint AUT (application under test), augmenting TAP-J stream
- [X] Add TAP-J analysis tools, to detect rates of flakiness in tests
- [ ] Add support for marking some tests as (implicitly?) new, forcing them to be run many times and pass every time
- [ ] Add support for marking tests as flaky, separating their results from the results of other tests
- [x] For tests that are failing flakily, show distribution of which line failed, test duration, version of code

### Continuous integration
- [ ] Add support for auto-filing issues (or updating existing issues) when a merged test fails that should not be flaky
- [ ] Suggests which flaky tests to debug first (based on heuristics)

### Local development
- [ ] Order test run during local development based on what's failed recently
- [ ] Line-level code coverage report
- [x] Rerunning tests during local development affected by what code you just modified (test code or AUT, using code coverage analysis)
- [ ] Line-level test rerunning, using code coverage
- [ ] Limit tests to files that are open in editor (open test files, open AUT files, etc)
- [ ] Can run with git-bisect to search for commit that introduced a bug
- [ ] Suggest which failing tests to debug first (based on heuristics)

### Correctness
- [ ] Add support to run tests in OS-specific sandbox for macOS
- [ ] Add support for overriding network syscalls (e.g. DNS, TCP connections)
- [ ] Add support for overriding time syscalls libfaketime
- [ ] Add support for overriding filesystem syscalls with charybdefs
- [ ] Provide a way to capture all network traffic generated by tests (e.g. with https://github.com/jonasdn/nsntrace)
- [ ] Provide a way to exactly reproduce failures (e.g. with Mozilla's rr)
