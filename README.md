# QA: The last (Ruby) test runner you'll ever need

QA is a lightweight tool for running your tests *fast*.

For years, the software testing ecosystem has lagged behind other parts of the software development pipeline. Advances in type systems, compiler technology, and prototyping environments (to name a few) have helped make software engineers much more productive. QA is an effort to make similar strides for automated testing tools.

## What can QA help me do today?

1. Run your tests faster. Run `qa run <type>` in your project directory and watch your test results scream by as they run in parallel. QA provides a beautiful, easy to understand report. No Rakefile necessary!

2. See which tests are slowing down your testrun. QA highlights tests that are dramatically slower than the average test duration. Look for the 🐌 at the end of successful testrun!

3. See per-test stderr and stdout. Even when running tests in parallel!

4. Investigate test performance by generating flamegraphs (or icicle graph) for the entire testrun. See `-save-flamegraph`, `-save-icegraph`, and `-save-palette`.

5. Run your tests in parallel. QA does this for you automatically. Use the `-squash` option to specify how tests are squashed into worker processes. The default is `file`. Use `all` to squish everything into a single worker process (effectively disabling parallel test running), or `none` to run every test in a separate worker process.

6. Analyze and eliminate [test flakiness](#whatis_flaky). The `-archive-base-dir` option for `qa run` records test outcomes across different runs. Use the `qa flaky` command with the same `-archive-base-dir` option to identify and diagnose flaky tests. This is new functionality, so please [open an issue](https://github.com/ajbouh/qa/issues/new) with questions and feedback!

7. Track threads, GC, require, SQL queries, and other noteworthy operations in a tracing format that can be used with the `chrome://tracing` tool, using `-save-trace` option.

8. See source code snippets and (with the experimental `-errors-capture-locals`) actual values of local variables for each from of an error's stack trace.

9. Record test output as TAP-J, using `-save-tapj` option.

10. Automatically partition Rails tests across multiple databases, one per worker (Using custom ActiveRecord integration logic). If the required test databases do not exist, they will be setup automatically before tests begin. NOTE This functionality is highly experimental. Disable it with `-warmup=false`. Please [open an issue](https://github.com/ajbouh/qa/issues/new) if you have trouble.

11. A faster and more focused development cycle. QA can prioritize test ordering based on the test files you're changing with `qa auto`.

## What languages and test frameworks does QA support?

Ruby's RSpec, MiniTest, test-unit. Be sure to use `bundle exec` when you run qa, if you're managing dependencies with Bundler. For example, if you're using rspec:
```
bundle exec qa run rspec
```

## What will QA help me do tomorrow?

1. An even faster and more focused development cycle. QA will prioritize test ordering based on dependencies of the files you're changing and what's failed recently.

2. Run your tests *even faster*. QA will package your test execution environment, run it on a massive fleet of remote machines (like [AWS Lambda](https://aws.amazon.com/lambda/)) and stream the results back to your terminal in real time.

3. Support more languages and testing frameworks. Please [open an issue](https://github.com/ajbouh/qa/issues/new) with your request!

## Getting started with QA

QA is not yet available via brew, apt, or any other software distribution repository. To start using it, you'll need to download the latest [release](https://github.com/ajbouh/qa/releases) and put the executable somewhere on your `PATH`.

You can also run the command below to download and unpack the latest release to the current directory.
```
curl -L -O $(curl -s https://api.github.com/repos/ajbouh/qa/releases/latest | grep "browser_" | grep -i $(uname -s) | cut -d\" -f4) && unzip qa-*.zip
```

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
> qa run minitest
...
> qa run -archive-base-dir ~/.qa/archive minitest
...
> qa flaky -archive-base-dir ~/.qa/archive
...
```

## Troubleshooting QA

Since QA is still in alpha, there are a number of rough edges.

If `qa run` seems to be acting strangely and isn't providing a reasonable error message, you may be experiencing a bug relating to swallowed error output. This is tied to QA's stdout and stderr capture logic. Adding the `-capture-standard-fds=false` option will disable the capture logic and should allow the original error to bubble up. Please [open an issue](https://github.com/ajbouh/qa/issues/new) with the error output.

## What are flaky tests?<a name="whatis_flaky"></a>

For a fast moving software team, automated tests are the last line of defense against shipping broken software to customers. But there's a dirty little secret in the world of automated testing: flaky tests. A flaky test fails intermittently with some (apparently random) probability. A flaky test doesn't care that it just passed 9 times in a row. On the 10th run, it will relish the chance to slowly drain your team's sanity.

Flaky tests sap your confidence in the rest of your tests. Their existence robs you of the peace of mind from seeing a test's "PASS" status. Every team has to battle flaky tests at some point, and few succeed in keeping them at bay. In fact, flakiness may wear you down to the point where you re-run a failed test, hoping that "MAYBE it is _just flaky_". Or worse, you comment it out, and suffer a customer-facing bug that would have been covered by the test!

So that's the bad news: by their very nature, flaky tests are hard to avoid. In many cases they start out looking like healthy tests. But when running on a machine under heavy load, they rear their ugly, randomly failing heads.  In some cases they may only fail when the network is saturated. (Which is a reason to avoid tests that rely on third party services in the first place.) Or the opposite could happen: you upgrade a dependency or language runtime to a faster version, and this speeds up the testrun enough to unveil latent flakiness you never recognized. Such are the perverse economics of flaky tests.

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
- [X] Fingerprint tests, augmenting TAP-J stream
- [ ] Fingerprint AUT (application under test), augmenting TAP-J stream
- [X] Add TAP-J analysis tools, to detect rates of flakiness in tests
- [ ] Add support for marking some tests as (implicitly?) new, forcing them to be run many times and pass every time
- [ ] Add support for marking tests as flaky, separating their results from the results of other tests
- [ ] For tests that are failing flakily, show distribution of which line failed, test duration, version of code

### Continuous integration
- [ ] Add support for auto-filing issues (or updating existing issues) when a merged test fails that should not be flaky
- [ ] Suggests which flaky tests to debug first (based on heuristics)

### Local development
- [x] Automatically run tests as you save changes to them.
- [ ] Automatically run tests as you save changes to any of their file-level dependencies.
- [ ] Automatically run tests as you change individual test methods or dependencies of those methods.
- [ ] Order test run during local development based on what's failed recently
- [ ] Line-level code coverage report
- [ ] Limit tests to files that are open in editor (open test files, open AUT files, etc)
- [ ] Can run with git-bisect to search for commit that introduced a bug
- [ ] Suggest which failing tests to debug first (based on heuristics)

### Correctness
- [ ] Add support to run tests in OS-specific sandbox for OS X
- [ ] Add support for overriding network syscalls (e.g. DNS, TCP connections)
- [ ] Add support for overriding time syscalls libfaketime
- [ ] Add support for overriding filesystem syscalls with charybdefs
- [ ] Provide a way to capture all network traffic generated by tests (e.g. with https://github.com/jonasdn/nsntrace)
- [ ] Provide a way to exactly reproduce failures (e.g. with Mozilla's rr)
