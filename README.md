# QA

QA is a lightweight tool for running your tests. It is designed to be language-agnostic, to minimize boilerplate and to speed up your debugging dev cycle.

The software testing ecosystem seems to lag behind other parts of the software development pipeline (e.g. type systems, compiler technology, prototyping environments etc. to name a few). This is why we cannot have nice things. It's time to change this. The goal of the QA project is to provide a common set of protocols and sane tools that make world-class testing practices accessible to everyone.
 
That's a lofty goal. As a start, we'll keep things simple.

## What can QA help me do today?

Nothing yet, because we haven't release any source code or binaries yet. :(

## What will QA help me do tomorrow?

1. Avoid boilerplate. QA will auto-detect your language and test framework, assemble everything needed to run your tests, and then run them. It will provide a beautiful, easy to interpret report of test outcomes.

2. A faster and more focused development cycle. QA will prioritize test ordering based on what's failed recently and what code you're changing.

3. Distribute your test execution across many machines, to get results much faster. QA will package up everything needed for your test execution environment to operate on a remote machine.

4. Analyze and eliminate [test flakiness](#whatis_flaky). By recording test outcomes across different runs, QA can integrate with existing tools (along with some new ones) to identify and diagnose problematic tests.

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
> qa
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

