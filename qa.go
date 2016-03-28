package main

// cd <basedir> && qa

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// TODO which ruby version must qa want to run?
func assert_must_have_ruby() {
	if err := exec.Command("which", "ruby").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "You don't have ruby on your system\n")
		os.Exit(1)
	}
}

// Assume this directory structure
//
// basedir/
//  lib/
//    foo.rb
//    ...
//  test/
//    test-foo.rb
//    test-*.rb
//    ...
func detect_and_run_ruby_tests() {
	assert_must_have_ruby()
	args := []string{
		"-I", "lib",
		"-e", `
tests = Dir.glob('./test/test[-_]*.rb').to_a
abort('QA: No tests found!') if tests.empty?

puts "QA: Found #{tests.size} test files"
require 'minitest/autorun'
tests.each {|t| require(t) }`}
	cmd := exec.Command("ruby", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			waitStatus := exitError.Sys().(syscall.WaitStatus)
			os.Exit(waitStatus.ExitStatus())
		}
	}
}

func main() {
	detect_and_run_ruby_tests()
}
