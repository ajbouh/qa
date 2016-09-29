#!/usr/bin/env ruby

require 'optparse'
require 'json'

# Print a summary of all failing tests, or, if provided, print the summary
# for a test_id.

require 'pp'

class Printer
  MESSAGE_WIDTH = 53
  DESCRIPTION_WIDTH = MESSAGE_WIDTH - 3
  OVERALL_HEADER_FORMAT =      "%3s %-#{MESSAGE_WIDTH}s  %5s  %5s  %5s  %8s  %s\n"
  OVERALL_FORMAT  =            "%3s \e[0;1m%-#{MESSAGE_WIDTH + 14}s\e[0m  %5s  %8s  %s\n"
  OUTCOMES_FORMAT =         "    %s\n"
  # OUTCOME_FORMAT  =    "    %s) %-#{MESSAGE_WIDTH - 3}s  %5s  %5s  %8s  %8s  %s\n"
  OUTCOME_FORMAT_BY_STATUS = Hash.new do |h, k|
    h[k] = "    #{color_by_status(k, '%s')}) #{color_by_status(k, "%-#{DESCRIPTION_WIDTH}s")}  %5s  %5s  %5s  %8s  %s\n"
  end

  ERROR_COLOR = 35
  FAIL_COLOR = 31
  PASS_COLOR = 32

  def self.color_by_status(status, string)
    case status
    when 'pass'
      color = PASS_COLOR
    when 'error'
      color = ERROR_COLOR
    when 'fail'
      color = FAIL_COLOR
    else
      return string
    end

    "\e[#{color};1m#{string}\e[0m"
  end

  def initialize
    @now = Time.now
  end

  def print_overall_header
    printf(OVERALL_HEADER_FORMAT, "", "Description", "E[Pr]", "Repro", "Count", "Last", "Location")
  end

  def print_overall(summary, summary_ix)
    total_count = summary["total-count"]

    prototype = summary["prototype"]["pass"] || summary["prototype"][summary["outcome-digests"].first]

    latest_timestamp = summary["prototype"].values.map { |p| p["timestamp"] || 0 }.max || 0
    ago = latest_timestamp > 0 ? ago_text(@now.to_f - latest_timestamp) : "?"

    file, line = prototype["file"], prototype["line"]
    filter = prototype["filter"]
    file = file.sub(Dir.pwd, '.') if file.start_with?(Dir.pwd + "/")

    printf(OVERALL_FORMAT,
        "#{summary_ix + 1})",
        summary["description"],
        "#{total_count}",
        ago,
        "#{file}:#{line}",
    )

    # Indent a little, then print a single letter for each observation
    outcomes = summary["outcome-sequence"].dup

    summary["outcome-index"].each do |digest, index|
      if digest == 'pass'
        pattern = /(\.)/
        status = 'pass'
      else
        pattern = /(#{index})/
        status = summary["status"][digest]
      end

      outcomes.gsub!(pattern, self.class.color_by_status(status, "\\1"))
    end

    printf(OUTCOMES_FORMAT, outcomes)
  end

  def print_outcome(summary, summary_ix, outcome_digest)
    get_summary_value = ->(k) do
      h = summary[k] || raise("no summary value '#{k}' for #{outcome_digest}")
      h[outcome_digest]
    end

    total_count = summary["total-count"]
    count = get_summary_value.("count")
    percent = ((count.to_f / total_count) * 100.0).round(0)
    message = ""

    prototype = get_summary_value.("prototype")
    filter = prototype['filter']
    file = prototype["file"]
    location = file
    if line = prototype["line"]
      location = "#{file}:#{line}"
    end

    status = prototype["status"]
    if exception = prototype["exception"]
      if status == "error"
        message = "#{exception["class"]}: "
      end

      message += exception['message'].gsub(/\n/, '↩ ')
      if backtrace = exception['backtrace']
        location_frame_ix = 0
        backtrace.each_with_index.find do |frame, ix|
          next false if frame['internal']

          location_frame_ix = ix
          true
        end

        if frame = backtrace[location_frame_ix]
          location = "#{frame['file']}:#{frame['line']}"
        end
      end
    end

    repro_run_limit = get_summary_value.("repro-run-limit")

    latest_timestamp = prototype["timestamp"] || 0
    ago = latest_timestamp > 0 ? ago_text(@now.to_f - latest_timestamp) : "?"

    if message.length > DESCRIPTION_WIDTH
      message = message[0..(DESCRIPTION_WIDTH - 3)] + " …"
    end

    printf(OUTCOME_FORMAT_BY_STATUS[status],
        get_summary_value.("outcome-index"),
        message,
        format_percentage(get_summary_value.("probability")),
        duration_text(get_summary_value.("repro-limit-expected-duration"), 0),
        "#{count}",
        ago,
        location,
    )
  end

  def print_summary(test_summaries, aces_count)
    print_overall_header

    puts "#{aces_count} other tests with no failures." if aces_count && aces_count > 0

    test_summaries.each_with_index { |summary, summary_ix| print_test_summary(summary, summary_ix) }
  end

  def print_test_id_detail(test_summaries, test_id)
    print_overall_header

    test = test_summaries.select { |v| v['id'] == test_id }
    test.each_with_index { |summary, summary_ix| print_test_summary(summary, summary_ix) }

    pp test
  end

  private

  def format_percentage(p)
    if p < 0.005
      "<1%"
    else
      "#{(p * 100).round(0)}%"
    end
  end

  def ago_text(duration, significant_units=1, second_precision=0)
    difference = duration
    s = ""

    days_f = difference/(3600.0*24.0)
    days = days_f.to_i
    if days > 0 && significant_units > 0
      days = days_f.round(0) if significant_units == 1
      s << "#{days}d"
      significant_units -= 1
    end

    hours_f = (difference%(3600.0*24.0)).to_f/3600.0
    hours = hours_f.to_i
    if hours > 0 && significant_units > 0
      hours = hours_f.round(0) if significant_units == 1
      s << "#{hours}h"
      significant_units -= 1
    end

    mins_f = (difference%(3600.0)).to_f/60.0
    mins = mins_f.to_i
    if mins > 0 && significant_units > 0
      mins = mins_f.round(0) if significant_units == 1
      s << "#{mins}m"
      significant_units -= 1
    end

    secs = (difference%60).to_f.round(second_precision)
    if secs > 0 && significant_units > 0
      s << "#{secs}s"
      significant_units -= 1
    end

    "#{s} ago"
  end


  def duration_text(duration, second_precision=3)
    difference = duration
    s = ""

    days = (difference/(3600*24)).to_i
    s << "#{days}d" if days > 0

    hours = ((difference%(3600*24))/3600).to_i
    s << "#{hours}h" if hours > 0

    mins = ((difference%(3600))/60).to_i
    s << "#{mins}m" if mins > 0

    secs = (difference%60).round(second_precision)
    s << "#{secs}s" if secs > 0

    if s.empty?
      msecs = ((difference%60) * 1000).round(second_precision)
      s << "#{msecs}ms"
    end

    s
  end

  def print_test_summary(summary, summary_ix)
    print_overall(summary, summary_ix)

    outcome_ix = 0
    summary["outcome-digests"].each do |outcome_digest|
      next if outcome_digest == 'pass'
      print_outcome(summary, summary_ix, outcome_digest)
      outcome_ix += 1
    end

    puts # Empty line between test summaries
  end
end

input_file = nil
format = 'pretty'
test_id = nil

opts = OptionParser.new do |opts|
  opts.banner = <<-EOH
Usage: tapj-report [OPTIONS]

Provided an input file or results from std in, aggregates and prints statistics
about tapj tests.

  EOH

  opts.on("--format pretty|json",
      "Specify whether to print a summary table or dump summary json."\
      "Defaults to #{format}") do |b|
    format = b
  end

  opts.on('-I', '--input PATH', "Input file for results") do |path|
    input_file = path
  end

  opts.on('-T', '--test-id TEST_ID',
      "Get add'l detail for test_id. Overridden by --format=json option."\
      " Can only be used for a single test_id.") do |id|
    test_id = id
  end
end

args = opts.parse(ARGV)
count = args[0] ? args[0].to_i : nil

test_summaries = []
begin
  file = input_file && File.open(input_file)
  input = file || $stdin
  input.each_line do |line|
    test_summaries.push(JSON.parse(line))
    break if count && test_summaries.length == count
  end
ensure
  file.close if file
end

case format.downcase
when 'pretty'
  printer = Printer.new

  if test_id
    printer.print_test_id_detail(test_summaries, test_id)
  else
    printer.print_summary(test_summaries, nil)
  end
when 'json'
  test_summaries.each do |test_summary|
    puts JSON.generate(test_summary)
  end
else
  abort "Unknown format: #{format}"
end
