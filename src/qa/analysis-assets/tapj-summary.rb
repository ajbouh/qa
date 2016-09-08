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

  def percentile(sorted, pcnt, &p)
    len = sorted.length
    (p.call(sorted[((len - 1) * pcnt).to_i]) + p.call(sorted[(len * pcnt).to_i])) / 2.0
  end

  def run_maths(hash_array, &p)
    sorted_hash_array = hash_array.sort_by(&p)
    {
      "mean" => mean(hash_array, &p),
      "min" => p.call(sorted_hash_array[0]),
      "median" => percentile(sorted_hash_array, 0.5, &p),
      "max" => p.call(sorted_hash_array[-1]),
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
  #     "total-count" => 1,
  #     "pass-count" => 1,
  #     "fail-count" => 0,
  #     "summary" => {
  #       "pass" => { "mean" => 3.454566, "median" => 3.454566, "std_dev" => 0,
  #           "observations" => 1 }
  #     }
  #   }
  # }
  OUTCOME_INDICES = "abcdefghijklmnopqrstuvwxyz"
  def summarize(test_results)
    test_summaries = summaries_by_test_id(test_results)

    test_summaries.each do |summary|
      observations = summary.delete("observations")

      # This dictates how we group test results
      groups = observations.group_by(&@subgroup_by_proc)

      groups.each do |outcome_digest, obs_array|
        set_summary_value = ->(k, v) do
          (summary[k] ||= {})[outcome_digest] = v
        end

        # Assume sampling the "prototype" observation for each group
        # is adequate.
        prototype = obs_array[0]
        set_summary_value.("status", prototype['status'])
        set_summary_value.("file", prototype['file'])
        set_summary_value.("line", prototype['line'])
        set_summary_value.("prototype", prototype)
        set_summary_value.("exception", prototype['exception'])

        Statistics.run_maths(obs_array, &@duration_proc).each do |k, v|
          begin
            set_summary_value.(k, v)
          rescue
            $stderr.puts "summary #{summary} k #{k} outcome_digest #{outcome_digest} v #{v}"
            raise
          end
        end
      end
      summary["outcome-digests"] = groups.keys
      summary["outcome-digests"].sort_by! do |outcome_digest|
        summary["count"][outcome_digest]
      end.reverse!

      # Indent a little, then print a single letter for each observation
      outcomes = ""
      outcome_indices = Hash.new { |h, k| h[k] = (OUTCOME_INDICES[h.size] || "!") }

      observations.each do |t|
        status = t["status"]
        case status
        when "pass"
          outcomes << "."
        else
          outcomes << outcome_indices[@subgroup_by_proc.call(t)]
        end
      end

      summary["outcome-sequence"] = outcomes
      summary["outcome-index"] = outcome_indices
      summary["total-count"] = observations.length
      summary["pass-count"] = observations.count(&@success_if_proc)
      summary["fail-count"] = observations.length - summary["pass-count"]
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
      summary = (test_summaries[id] ||= {"id" => id, "observations" => []})
      summary["observations"].push(t)
    end

    # Sort observations.
    test_summaries.each do |test_id, summary|
      summary["observations"].sort_by!(&@sort_by_proc)
    end

    test_summaries.values
  end
end

# Print a summary of all failing tests, or, if provided, print the summary
# for a test_id.

require 'pp'

class Printer
  MESSAGE_WIDTH = 70
  MESSAGE_FORMAT = "%-70s"
  OVERALL_HEADER_FORMAT = "%3s %-#{MESSAGE_WIDTH}s  %4s  %5s  %s\n"
  OVERALL_FORMAT  = "%3s \e[0;1m%-#{MESSAGE_WIDTH + 6}s\e[0m  %5s  %s\n"
  OUTCOMES_FORMAT = "%3s %-#{MESSAGE_WIDTH + 6}s  %5s  %s\n"
  OUTCOME_FORMAT  = "    %s) %-#{MESSAGE_WIDTH - 3}s  %4s  %5s  %s\n"

  def print_overall_header
    printf(OVERALL_HEADER_FORMAT, "", "Description", "Rate", "Count", "Location")
  end

  def print_overall(summary, summary_ix)
    total_count = summary["total-count"]

    file, line = summary["file"]["pass"], summary["line"]["pass"]
    file = file.sub(Dir.pwd, '.') if file.start_with?(Dir.pwd + "/")
    printf(OVERALL_FORMAT,
        "#{summary_ix + 1})",
        summary["id"].flatten.compact.join(" ▸ "),
        "#{total_count}",
        "#{file}:#{line}",
    )

    # Indent a little, then print a single letter for each observation
    outcomes = sprintf(MESSAGE_FORMAT, summary["outcome-sequence"].dup)
    outcomes.gsub!(/([^\.])/, "\e[31;1m\\1\e[0m")
    outcomes.gsub!(/\./, "\e[32;1m.\e[0m")

    printf(OUTCOMES_FORMAT, "", outcomes, "", "", "", "")

    # "  #{outcome_digest}", # Indent a little
    # "Mean", "Min", "Md", "Max"
    # summary["mean"][outcome_digest].round(3),
    # summary["min"][outcome_digest].round(3),
    # summary["median"][outcome_digest].round(3),
    # summary["max"][outcome_digest].round(3),
  end

  def print_outcome(summary, outcome_digest)
    get_summary_value = ->(k) do
      h = summary[k] || raise("no summary value '#{k}' for #{outcome_digest}")
      h[outcome_digest]
    end

    total_count = summary["total-count"]
    count = get_summary_value.("count")
    percent = ((count.to_f / total_count) * 100.0).round(0)
    message = ""

    location = get_summary_value.("file")
    if line = get_summary_value.("line")
      location = "#{location}:#{line}"
    end

    if exception = get_summary_value.("exception")
      message = exception['message'].gsub(/\n/, '↩')
      if backtrace = exception['backtrace']
        if frame = backtrace[0]
          location = "#{frame['file']}:#{frame['line']}"
        end
      end
    end

    printf(OUTCOME_FORMAT,
        get_summary_value.("outcome-index"),
        message,
        "#{percent}%",
        "#{count}",
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

  def format_percentage(top, bottom)
    return "#{((top.to_f/bottom.to_f) * 100).to_i}%" if bottom != 0

    "n/a"
  end

  def print_test_summary(summary, summary_ix)
    print_overall(summary, summary_ix)

    outcome_ix = 0
    summary["outcome-digests"].each do |outcome_digest|
      next if outcome_digest == 'pass'
      print_outcome(summary, outcome_digest)
      outcome_ix += 1
    end

    puts # Empty line between test summaries
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
      " Defaults to printing details.") do |b|
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
test_summaries.sort_by! { |v| [v["fail-count"], v["total-count"], v["id"]] }.reverse

if show_aces
  show = test_summaries
else
  show = test_summaries.select { |v| v["fail-count"] > 0 }
  hidden_aces = nil
end

case format.downcase
when 'pretty'
  printer = Printer.new

  if test_id
    printer.print_test_id_detail(show, test_id)
  else
    printer.print_summary(
      show,
      hidden_aces && hidden_aces.length)
  end
when 'json'
  puts JSON.generate(show)
else
  abort "Unknown format: #{format}"
end
