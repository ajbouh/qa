# QA

The state of the art in software testing is quite rudimentary compared to other stages in the software development pipeline. We have great prototyping environments, type systems, compiler technology, network protocols, storage systems, dynamic runtimes, graphics stacks, compression, to name a few. But the software testing ecosystem lacks even basic de facto standards for protocols and language-agnostic tools.

It's time to change that.

The goal of the QA project is to provide a common set of protocols and tools that make world-class testing practices accessible to everyone.

That's a lofty goal. To start with we'll keep things simple.

## What can QA help me do today?

Nothing yet, because we haven't release any source code or binaries yet. :(

## What will QA help me do tomorrow?

1. Avoid boilerplate. QA will auto-detect your language and test framework, assemble everything needed to run your tests, and then run them. It will provide a beautiful, easy to interpret report of test outcomes.

2. A faster and more focused development cycle. QA will prioritize test ordering based on what's failed recently and what code you're changing.

3. Distribute your test execution across many machines, to get results much faster. QA will package up everything needed for your test execution environment to operate on a remote machine.

4. Analyze and eliminate test flakiness. By recording test outcomes across different runs, QA can integrate with existing tools (along with some new ones) to identify and diagnose problematic tests.

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
