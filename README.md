# QA: The last (Ruby) test runner you'll ever need

QA is a lightweight tool for running your tests. It is designed to help you write higher quality code more quickly and with less drudgery. We aspire to make world-class testing practices, like auto-parallelizing tests across hundreds of machines, accessible to everyone.

For years, the software testing ecosystem has lagged behind other parts of the software development pipeline (e.g. type systems, compiler technology, prototyping environments etc. to name a few). It's time to change this.

That's a lofty goal. As a start, we'll keep things simple.

## What can QA help me do today?

1. Run your tests faster. Run `qa run <type>:<glob>` in your project directory and watch your test results scream by as they run in parallel. QA provides a beautiful, easy to understand report. No Rakefile necessary!

2. See which tests are slowing down your testrun. QA highlights tests that are dramatically slower than the average test duration. Look for the 🐌 at the end of successful testrun!

3. See which test-specific stderr and stdout, even when tests are run in parallel! generated it.

4. Generate a flamegraph (or icicle graph) for the entire testrun, using `-save-flamegraph`, `-save-icegraph`. See also `-save-palette`.

5. Run your tests in parallel. QA does this for you automatically, for test types `rspec`, `rspec-pendantic`, `minitest`, `minitest-pendantic`, `test-unit`, and `test-unit-pendantic`. The `-pendantic` suffix runs each test method in its own worker process. (The default is to run each test case in its own worker fork.)

6. Track threads, GC, require, SQL queries, and other noteworthy operations in a tracing format that can be used with the `chrome://tracing` tool, using `-save-trace` option.

7. If a test fails, see source code snippets and, in some cases, actual values of local variables (use option `-errors-capture-locals`, experimental and Mac OS X only) for for each entry in the stack trace.

8. Record test output as TAP-J, using `-save-tapj` option.

9. Special ActiveRecord integration means QA will automatically partition tests across multiple databases, one per worker. If the required test databases do not exist, they will be setup automatically before tests begin. NOTE This functionality is highly experimental.

## What languages and test frameworks does QA support?

Ruby's RSpec, MiniTest, test-unit. Be sure to use `bundle exec` when you run qa, if you're managing dependencies with Bundler.

Please open an issue to request other languages and frameworks!

## What will QA help me do tomorrow?

1. Analyze and eliminate [test flakiness](#whatis_flaky). By recording test outcomes across different runs, QA can integrate with existing tools (along with some new ones) to identify and diagnose flaky, slow, and otherwise problematic tests.

2. A faster and more focused development cycle. QA will prioritize test ordering based on what's failed recently and what files you're changing.

3. Run your tests *even faster*. QA will package your test execution environment, run it on a massive fleet of remote machines and stream the results back to your terminal in real time.


## Getting started with QA

Download the latest release and put the executable somewhere on your `PATH`. Switch to your project directory and run `qa`. See below for an example.

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
> qa run minitest:test/test**.rb
???
```

## What are flaky tests?<a name="whatis_flaky"></a>

For a fast moving software team, automated tests are the last line of defense against shipping broken software to customers. But there's a dirty little secret in the world of automated testing: flaky tests. A flaky test fails intermittently with some (apparently random) probability. A flaky test doesn't care that it just passed 9 times in a row. On the 10th run, it will relish the chance to slowly drain your team's sanity.

Flaky tests sap your confidence in the rest of your tests. Their existence robs you of the peace of mind from seeing a test's "PASS" status. Every team has to battle flaky tests at some point, and few succeed in keeping them at bay. In fact, flakiness may wear you down to the point where you re-run a failed test, hoping that "MAYBE it is _just flaky_". Or worse, you comment it out, and suffer a customer-facing bug that would have been covered by the test!

So that's the bad news: by their very nature, flaky tests are hard to avoid. In many cases they start out looking like healthy tests. But when running on a machine under heavy load, they rear their ugly, randomly failing heads.  In some cases they may only fail when the network is saturated. (Which is a reason to avoid tests that rely on third party services in the first place.) Or the opposite could happen: you upgrade a dependency or language runtime to a faster version, and this speeds up testrun enough to unveil latent flakiness you never recognized. Such are the perverse economics of flaky tests.

## How will QA help me with test flakiness?
Now the good news: with QA, we've set out to address the shortcomings we see with today's testing tools. We want a toolset that's *fast* and gives us more firepower for dealing with the reality of flaky tests.

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
- [ ] Fingerprint tests, augmenting TAP-J stream
- [ ] Fingerprint AUT (application under test), augmenting TAP-J stream
- [ ] Add TAP-J analysis tools, to detect rates of flakiness in tests
- [ ] Add support for marking some tests as (implicitly?) new, forcing them to be run many times and pass every time
- [ ] Add support for marking tests as flaky, separating their results from the results of other tests
- [ ] For tests that are failing (flaky or not), shows distribution of which line failed, test duration, version of code

### Continuous integration
- [ ] Add support for auto-filing issues (or updating existing issues) when a merged test fails that should not be flaky
- [ ] Suggests which flaky tests to debug first (based on heuristics)

### Local development
- [ ] Order test run during local development based on what's failed recently
- [ ] Line-level code coverage report
- [ ] Rerunning tests during local development affected by what code you just modified (test code or AUT, using code coverage analysis)
- [ ] Limit tests to files that are open in editor (open test files, open AUT files, etc)
- [ ] Can run with git-bisect to search for commit that introduced a bug
- [ ] Suggest which failing tests to debug first (based on heuristics)

### Correctness
- [ ] Add support to run tests in OS-specific sandbox for OS X
- [ ] Add support for overriding network syscalls (e.g. DNS, TCP connections)
- [ ] Add support for overriding time syscalls libfaketime
- [ ] Add support for overriding filesystem syscalls with charybdefs
- [ ] Provide a way to exactly reproduce failures (e.g. with Mozilla's rr)
