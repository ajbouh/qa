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

  # Based on https://arxiv.org/pdf/1105.1486v1.pdf
  def estimate_probability(count, total)
    (count.to_f + 1.0) / (total.to_f + 2.0)
  end

  def min_runs_needed_to_probably_repro(pr_of_single_repro, pr_of_eventual_repro)
    pr_of_never_repro = 1.0 - pr_of_eventual_repro
    pr_of_single_repro_fail = 1.0 - pr_of_single_repro

    Math.log(pr_of_never_repro, pr_of_single_repro_fail).ceil
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
      total_count = observations.length
      total_duration = observations.inject(0.0) { |sum, obs| sum + @duration_proc.call(obs) }
      mean_duration = total_duration.to_f / total_count.to_f
      summary["total-count"] = total_count
      summary["total-duration"] = total_duration
      summary["pass-count"] = observations.count(&@success_if_proc)
      summary["fail-count"] = total_count - summary["pass-count"]

      # This dictates how we group test results
      groups = observations.group_by(&@subgroup_by_proc)

      expected_run_dur_num = 0
      expected_run_dur_den = 0

      groups.each do |outcome_digest, obs_array|
        set_summary_value = ->(k, v) do
          (summary[k] ||= {})[outcome_digest] = v
        end

        # Assume sampling the "prototype" observation for each group
        # is adequate.
        prototype = obs_array.max { |a, b| (a["timestamp"] || 0) <=> (b["timestamp"] || 0) }
        set_summary_value.("prototype", prototype)
        set_summary_value.("status", prototype['status'])

        count = obs_array.length
        set_summary_value.("count", count)
        pr = estimate_probability(count, total_count)

        limit_repro_pr = 0.999

        set_summary_value.("probability", pr)
        set_summary_value.("repro-limit-probability", limit_repro_pr)
        set_summary_value.("repro-run-limit", min_runs_needed_to_probably_repro(pr, limit_repro_pr))

        stats = Statistics.run_maths(obs_array, &@duration_proc)
        expected_run_dur_num += stats['mean'] * pr
        expected_run_dur_den += pr

        stats.each do |k, v|
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
      # Assign outcome indices in sorted order.
      outcome_indices = Hash.new { |h, k| h[k] = (OUTCOME_INDICES[h.size] || "!") }
      summary["outcome-digests"].each do |outcome_digest|
        next if outcome_digest == "pass"
        outcome_indices[outcome_digest]
      end
      summary["outcome-index"] = outcome_indices

      expected_run_duration = expected_run_dur_num.to_f / expected_run_dur_den.to_f

      groups.each_key do |outcome_digest|
        set_summary_value = ->(k, v) do
          (summary[k] ||= {})[outcome_digest] = v
        end

        get_summary_value = ->(k) { summary[k][outcome_digest] }

        repro_runs = get_summary_value.("repro-run-limit")
        set_summary_value.("repro-limit-expected-duration", expected_run_duration * repro_runs)
      end

      # Indent a little, then print a single letter for each observation
      outcomes = ""
      observations.each do |t|
        status = t["status"]
        case status
        when "pass"
          outcomes << "."
        else
          outcomes << outcome_indices[@subgroup_by_proc.call(t)]
        end
      end

      summary["description"] = summary["id"].flatten.compact.join(" ▸ ")

      summary["outcome-sequence"] = outcomes
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

summarizer = Summarizer.new
input_file = nil
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

show_aces = true

opts = OptionParser.new do |opts|
  opts.banner = <<-EOH
Usage: tapj-summary [OPTIONS]

Provided an input file or results from std in, aggregates and prints statistics
about tapj tests.

  EOH

  opts.on("--[no-]show-aces",
      "Specify whether to print details for tests without failures."\
      " Defaults to printing details.") do |b|
    show_aces = b
  end

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

  opts.on('-I', '--input PATH', "Input file for results") do |path|
    input_file = path
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
test_summaries.select! { |v| v["fail-count"] > 0 } unless show_aces
test_summaries.sort_by! { |v| [v["fail-count"], v["total-count"], v["id"]] }.reverse!

test_summaries.each do |summary|
  puts JSON.generate(summary)
end
