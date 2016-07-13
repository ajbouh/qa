#!/usr/bin/env ruby

require 'optparse'
require 'json'

# Given an array of hashes, performs statistics on values at the provided key.
module Statistics
  module_function

  def sum(hash_array)
    hash_array.inject(0) { |s, n| s + yield(n) }
  end

  def mean(hash_array, &p)
    sum(hash_array, &p) / hash_array.length.to_f
  end

  def variance(hash_array, &p)
    m = mean(hash_array, &p)
    sum = hash_array.inject(0) { |s, n| s + (p.call(n) - m)**2 }

    return sum/(hash_array.length - 1).to_f if hash_array.length > 1

    0
  end

  def std_dev(hash_array, &p)
    Math.sqrt(variance(hash_array, &p))
  end

  def median(hash_array, &p)
    sorted = hash_array.sort_by(&p)
    len = sorted.length

    (p.call(sorted[(len - 1) / 2]) + p.call(sorted[len / 2])) / 2.0
  end

  def run_maths(hash_array, &p)
    {
      "mean" => mean(hash_array, &p),
      "median" => median(hash_array, &p),
      "std_dev" => std_dev(hash_array, &p),
      "count" => hash_array.length
    }
  end
end

# Given an array of test result objects, returns a hash of tests, keyed by
# test id and whose value includes an array of observations and a set of
# statistics about the results of that test.
class Summarizer
  PASS = "pass"
  FAIL = "fail"
  ERROR = "error"
  SKIP = "skip"

  attr_accessor :duration_proc
  attr_accessor :sort_by_proc
  attr_accessor :group_by_proc
  attr_accessor :ignore_if_proc
  attr_accessor :success_if_proc
  attr_accessor :subgroup_by_proc

  def initialize
    @duration_proc = nil
    @sort_by_proc = nil
    @group_by_proc = nil
    @ignore_if_proc = nil
    @subgroup_by_proc = nil
  end

  # Calculates stats from test observations.
  # Returns an updated test summary which includes a summary object.
  # The summary object is keyed by outcome (which may include exception
  # details) and whose value includes stats.
  # Example:
  # { "platform/spin/verification/ui-conversation-window+macosx+x86_64:SkyonBlacklistTest:test_skyon_blacklist" => {
  #     "observations" => [{ "status" => "pass", "time" => 3.454566 }],
  #     "total_count" => 1,
  #     "pass_count" => 1,
  #     "fail_count" => 0,
  #     "summary" => {
  #       "pass" => { "mean" => 3.454566, "median" => 3.454566, "std_dev" => 0,
  #           "observations" => 1 }
  #     }
  #   }
  # }
  def summarize(test_results)
    test_summaries = summaries_by_test_id(test_results)

    test_summaries.each do |summary|
      observations = summary.delete("observations")

      # This dictates how we group test results
      groups = observations.group_by(&@subgroup_by_proc)

      groups.each do |outcome_digest, obs_array|
        Statistics.run_maths(obs_array, &@duration_proc).each do |k, v|
          begin
            (summary[k] ||= {})[outcome_digest] = v
          rescue
            $stderr.puts "summary #{summary} k #{k} outcome_digest #{outcome_digest} v #{v}"
            raise
          end
        end
      end
      summary["outcome_digests"] = groups.keys

      summary["total_count"] = observations.length
      summary["pass_count"] = observations.count(&@success_if_proc)
      summary["fail_count"] = observations.length - summary["pass_count"]
    end

    test_summaries
  end

  private

  # Summarizes test by id.
  # Returns object where key is the concatenation of suite + case + test
  # and the value includes an array of observation objects.
  # Example:
  # [
  #   {
  #     "id" => ["platform/spin/verification/ui-conversation-window", "SkyonBlacklistTest", "test_skyon_blacklist"],
  #     "observations" => [{ "status" => "pass", "time" => 3.454566 }, ...]
  #   },
  #   ...
  # ]
  def summaries_by_test_id(test_results)
    test_summaries = {}

    test_results.each do |t|
      # This dictates how we group tests
      next if @ignore_if_proc.call(t)

      id = @group_by_proc.call(t)
      test_summary = (test_summaries[id] ||= {"id" => id, "observations" => []})
      test_summary["observations"].push(t)
    end

    # Sort observations.
    test_summaries.each do |test_id, test_summary|
      test_summary["observations"].sort_by!(&@sort_by_proc)
    end

    test_summaries.values
  end
end

# Print a summary of all failing tests, or, if provided, print the summary
# for a test_id.

require 'pp'

class Printer
  def print_summary(test_summaries, other_count)
    format = "%8s %8s  %-100s %15s %15s %15s\n"
    printf(format,
        "PassRt", "Count", "Description", "Mean (s)", "Med (s)", "StdDev (s)")

    test_summaries.each { |details| print_test_summary(details, format) }

    puts "#{other_count} other tests with no failures." if other_count > 0
  end

  def print_test_id_detail(test_summaries, test_id)
    format = "%8s %8s  %-100s %15s %15s %15s\n"
    printf(format,
        "PassRt", "Count", "Description", "Mean (s)", "Med (s)", "StdDev (s)")

    test = test_summaries.select { |v| v['id'] == test_id }
    test.each { |details| print_test_summary(details, format) }

    pp test
  end

  private

  def format_percentage(top, bottom)
    return "#{((top.to_f/bottom.to_f) * 100).to_i}%" if bottom != 0

    "n/a"
  end

  def print_test_summary(test_details, format)
    total_count = test_details["total_count"]
    pass_count = test_details["pass_count"]
    pass_rate = format_percentage(pass_count, total_count)

    printf(format,
      pass_rate,
      "#{pass_count}/#{total_count}",
      test_details["id"],
      "",
      "",
      ""
    )

    # Indent a little, then print a single letter for each observation
    outcomes = ""
    test_details["observations"].each do |t|
      status = t["status"]
      case status
      when "pass"
        outcomes << "."
      else
        outcomes << status[0].upcase
      end
    end
    printf(format, "", "", outcomes, "", "", "")

    outcome_digests = test_details["outcome_digests"].sort_by do |outcome_digest|
      test_details["count"][outcome_digest]
    end.reverse

    outcome_digests.each do |outcome_digest|
      printf(format,
          "",
          stats["observations"][outcome_digest],
          "   #{stats["statuses"][outcome_digest]}", # Indent a little
          stats["mean"][outcome_digest].round(1),
          stats["median"][outcome_digest].round(1),
          stats["std_dev"][outcome_digest].round(1),
      )
    end

    printf(format, "", "", "", "", "", "") # Empty line between test summaries
  end
end

summarizer = Summarizer.new
input_file = nil
format = 'pretty'
show_aces = true
test_id = nil
test_results = nil
ignore_if_procs = []
success_if_procs = []

# Return proc that returns value of field.
def parse_field(key)
  keys = key.split('.')

  ->(t) do
    # Get value based on sequence of key lookups.
    keys.inject(t) do |a, k|
      begin
        a[k]
      rescue
        raise "No field #{k} for #{a.inspect} (field: #{key}, object: #{t.inspect})"
      end
    end
  end
end

# Return proc that evaluates field==value for object.
def parse_predicate(pred)
  key, value = pred.split('==', 2)
  key_proc = parse_field(key)
  value = JSON.parse("[#{value}]").first rescue value

  ->(t) do
    key_proc.call(t) == value
  end
end

def any_proc(procs)
  ->(t) do
    procs.any? { |p| p.call(t) }
  end
end

opts = OptionParser.new do |opts|
  opts.banner = <<-EOH
Usage: tapj-summary [OPTIONS]

Provided an input file or results from std in, aggregates and prints statistics
about tapj tests.

  EOH

  opts.on("--duration KEY", "") do |arg|
    summarizer.duration_proc = parse_field(arg)
  end

  opts.on("--sort-by KEY", "") do |arg|
    summarizer.sort_by_proc = parse_field(arg)
  end

  opts.on("--group-by KEY", "") do |keys|
    field_procs = keys.split(',').map { |key| parse_field(key) }

    summarizer.group_by_proc = ->(t) do
      # Get value based on sequence of key lookups.
      field_procs.map { |p| p.call(t) }
    end
  end

  opts.on("--subgroup-by KEY", "") do |arg|
    summarizer.subgroup_by_proc = parse_field(arg)
  end

  opts.on("--ignore-if KEY==VALUE", "") do |arg|
    ignore_if_procs.push(parse_predicate(arg))
  end

  opts.on("--success-if KEY==VALUE", "") do |arg|
    success_if_procs.push(parse_predicate(arg))
  end

  opts.on("--format pretty|json",
      "Specify whether to print a summary table or dump summary json."\
      "Defaults to #{format}") do |b|
    format = b
  end

  opts.on("--[no-]show-aces",
      "Specify whether to print details for tests without failures."\
      " Defaults to not printing details.") do |b|
    show_aces = b
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

opts.parse!(ARGV)

summarizer.ignore_if_proc = any_proc(ignore_if_procs)
summarizer.success_if_proc = any_proc(success_if_procs)

test_results = []
begin
  file = input_file && File.open(input_file)
  input = file || $stdin
  input.each_line do |line|
    test_results.push(JSON.parse(line))
  end
ensure
  file.close if file
end

test_summaries = summarizer.summarize(test_results)
test_summaries.sort_by! { |v| [v["fail_count"], v["total_count"], v["median"].values.max] }.reverse

if show_aces
  show = test_summaries
else
  show = test_summaries.select { |v| v["fail_count"] > 0 }
  aces = test_summaries.select { |v| v["fail_count"] == 0 }
end


case format.downcase
when 'pretty'
  printer = Printer.new

  if test_id
    printer.print_test_id_detail(show, test_id)
  else
    printer.print_summary(show, aces.length)
  end
when 'json'
  puts JSON.generate(show)
else
  abort "Unknown format: #{format}"
end
