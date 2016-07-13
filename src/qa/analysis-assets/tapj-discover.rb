#!/usr/bin/env ruby

require 'optparse'
require 'json'
require 'date'
require 'digest/sha1'

# Scans provided audit_directory for tapj files within directories that fall
# between the provided date range.
class Qualifier

  TAPJ_EXTENSION = ".tapj"

  def initialize(days_back, until_date, audit_directory)
    @days_back = days_back
    @until_date = until_date
    @audit_directory = audit_directory
  end

  # Given root paths of builds, read and parse tapj files
  def tapj_file_list
    tapj_files = []

    # Filter to only latest build dirs so we don't scan the whole directory
    qualifying_directories.each do |d|
      tapj_files.concat(Dir[File.join(d, '**', "*#{TAPJ_EXTENSION}")])
    end

    tapj_files
  end

  private

  # Return root paths of builds from last N days
  def qualifying_directories
    dirs = []
    target = @until_date - @days_back + 1

    while target <= @until_date
      dir = File.join(@audit_directory, target.strftime("%Y-%m-%d"))
      dirs.push(dir) if File.directory?(dir)
      target = target.next
    end

    dirs
  end
end

# Provided a list of tapj files, returns a parsed array of test objects, each
# having details of a single test result
class Collector

  TAPJ_SUITE_TYPE = "suite"
  TAPJ_CASE_TYPE = "case"
  TAPJ_TEST_TYPE = "test"

  def initialize(tapj_files)
    @tapj_files = tapj_files
  end

  # Read tapj file and parse.
  # This adds new fields for the suite and case to any test-level output.
  def parse_tapj_output
    @tapj_files.each do |f|
      current_suite = nil
      current_case_labels = []

      File.open(f) do |io|
        io.each_line do |line|
          event = JSON.parse(line)

          case event["type"]
          when TAPJ_SUITE_TYPE
            current_suite = event
            current_suite.freeze
          when TAPJ_CASE_TYPE
            level = event['level'] || 0
            current_case_labels = current_case_labels.dup
            if level < current_case_labels.length
              current_case_labels.pop(current_case_labels.length - level)
            end
            current_case_labels.push(event['label'])
            current_case_labels.freeze
          when TAPJ_TEST_TYPE
            event["suite"] = current_suite
            event["case-labels"] = current_case_labels
            event["outcome-digest"] = outcome_digest(event)
            yield event
          end
        end
      end
    end
  end

  private

  def outcome_digest(event)
    status = event["status"]
    return status unless "error" == status || "fail" == status

    pruned = {}
    if exception = event["exception"]
      pruned["class"] = exception["class"]
      snippets = event["snippets"]
      if backtrace = exception["backtrace"]
        include_source = false
        pruned["frames"] = backtrace.map do |frame|
          h = {}
          file = frame["file"]
          line = frame["line"]
          h["file"] = file if file
          h["method"] = frame["method"] if frame["method"]
          if snippets && file && line
            line_source = snippets[file][line]
            h["line-source"] = line_source if line_source
          end

          h
        end
      else
        pruned["message"] = exception["message"]
      end
    end

    digest = Digest::SHA1.hexdigest(JSON.generate(pruned))

    return "#{status}:#{digest}"
  end
end

days_in_window = 7
audit_directory = nil
output_file = nil
until_date = Date.today
date_format = 'YYYY-MM-DD'

opts = OptionParser.new do |opts|
  opts.banner = <<-EOH
Usage: tapj-flatten [OPTIONS]

Scans specified directory for subdirectories named as #{date_format},
parses TAP-J files within them and enriches them with "case", "suite",
and "outcome-digest" fields in each test event, and sends output to
stdout or a specified json file.

  EOH

  opts.on('-N', '--number-days NUM_DAYS',
      "Number of days preceding until-date option to aggregate. " +
      "(default: #{days_in_window})") do |days|
    days_in_window = days.to_i
  end

  opts.on('-U', '--until-date DATE',
      "Most recent date to aggregate until. " +
      "Format: #{date_format} (default: #{until_date})") do |date|
    until_date = Date.strptime(date, "%Y-%m-%d")
  end

  opts.on('-D', '--dir PATH', "Path to directory of tapj output. Required.") do |dir|
    audit_directory = dir
  end

  opts.on('-O', '--output PATH', "Output file for results. Default is stdout.") do |path|
    output_file = path
  end
end

opts.parse!(ARGV)

unless audit_directory
  abort "Missing --dir option"
end

start = Time.now
qualifier = Qualifier.new(days_in_window, until_date, audit_directory)
tapj_file_list = qualifier.tapj_file_list
$stderr.puts("#{Time.now - start}s to find #{tapj_file_list.length} files")

# Let the user know we're parsing
start = Time.now
parser = Collector.new(tapj_file_list)

begin
  file = output_file && File.open(output_file, 'w')
  out = file || $stdout
  parser.parse_tapj_output do |test|
    out.puts(JSON.generate(test))
    out.flush
    $stderr.write(".")
  end

  $stderr.puts("\n#{Time.now - start}s to parse #{tapj_file_list.length} files")
ensure
  file.close if file
end
