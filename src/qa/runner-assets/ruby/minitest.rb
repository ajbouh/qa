gem "minitest"

require 'minitest'
require 'stringio'
require 'mutex_m'
require 'json'

# For Minitest::Unit::VERSION == "4.3.2" (bundled with Ruby 2.0.0)
# See: https://github.com/seattlerb/minitest/tree/f48ef8ffc0ff2992e2515529f1e4a9dcc1eeca3f

# But the below was written for Minitest::Unit::VERSION == "5.8.4"

class TapJRunner
  # TAP-Y/J Revision
  REVISION = 4

  # Backtrace patterns to be omitted.
  # Consider adding regexp that matches this file
  IGNORE_CALLERS = []

  include Mutex_m

  def initialize(options = {})
    @io      = options.delete(:io) || $stdout
    @options = options

    @assertions = 0
    @count      = 0
    @results = []
    @suite_start_time = nil
    @test_count = nil

    @previous_case = nil
  end

  #
  # Minitest's initial hook ran just before testing begins.
  #
  def start
    @suite_start_time = Time.now

    @test_cases = Minitest::Runnable.runnables
    count_tests!(@test_cases)

    capture_io

    puts_json(
        'type'  => 'suite',
        'start' => @suite_start_time.strftime('%Y-%m-%d %H:%M:%S'),
        'count' => @test_count,
        'seed'  => @options[:seed],
        'rev'   => REVISION)
  end

  #
  # Process a test result.
  #
  def record(result)
    @count += 1
    @assertions += result.assertions

    @results << result

    if @previous_case != result.class
      puts_json(
          'type'    => 'case',
          'subtype' => '',
          'label'   => "#{result.class}",
          'level'   => 0)
    end

    if Minitest.const_defined?(:Spec) && @result.class < Minitest::Spec
      label = result.name.sub(/^test_\d+_/, '').gsub('_', ' ')
    else
      label = result.name
    end

    doc = {
      'type'        => 'test',
      'subtype'     => '',
      'filter'      => "#{result.class}##{result.name}",
      'file'        => result.method(result.name).source_location[0], # returns [file, line]
      'label'       => "#{label}",
      'time' => result.time
    }

    record_stdcom(doc)

    exception = result.failure

    case exception
    when Minitest::Skip
      doc['status'] = 'todo'
    when Minitest::UnexpectedError
      doc['status'] = 'error'
    when Minitest::Assertion
      doc['status'] = 'fail'
    when nil
      doc['status'] = 'pass'
    end

    if exception
      doc['exception'] = TapjExceptions.summarize_exception(
          exception.error, exception.backtrace)
    end

    puts_json(doc)

    @previous_case = result.class
  end

  #
  # Minitest's finalization hook.
  #
  def report
    aggregate = @results.group_by { |r| r.failure.class }
    aggregate.default = [] # dumb. group_by should provide this

    uncapture_io

    corrected_time = Time.now - @suite_start_time
    puts_json(
        'type' => 'final',
        'time' => corrected_time,
        'counts' => {
          'total' => @test_count,
          'pass'  => aggregate[NilClass].size,
          'fail'  => aggregate[Minitest::Assertion].size,
          'error' => aggregate[Minitest::UnexpectedError].size,
          'omit'  => 0, # "omitted" tests are omitted by design
          # "pending" tests are tests that call skip() which shall be implemented someday.
          'todo'  => aggregate[Minitest::Skip].size
        })
  end

  def passed? # :nodoc:
    @results.all? { |r| r.skipped? || r.passed? }
  end

  private

  def puts_json(obj)
    @io.puts("#{JSON.generate(obj)}\n")
    @io.flush
  end

  def count_tests!(test_cases)
    filter = @options[:filter] || '/./'
    filter = Regexp.new $1 if filter =~ /\/(.*)\//

    @test_count = test_cases.inject(0) do |acc, test_case|
      acc + test_case.runnable_methods.grep(filter).length
    end
  end

  def record_stdcom(doc)
    doc['stdout'] = $stdout.string unless $stdout.length == 0 #empty?
    doc['stderr'] = $stderr.string unless $stderr.length == 0 #empty?
    $stdout.close; $stderr.close
    $stdout, $stderr = StringIO.new, StringIO.new
  rescue
    uncapture_io
    raise
  end

  def capture_io
    @_stdout, @_stderr = $stdout, $stderr
    $stdout, $stderr = StringIO.new, StringIO.new
  end

  def uncapture_io
    $stdout, $stderr = @_stdout, @_stderr
  end
end


options = {
  # NB(xyu.2015-01-21): Only the test reporter may output to stdout; prevent child
  # processes from polluting stdout by redirecting it to stderr. Otherwise, child
  # processes that are not properly cleaned up could leak stdout.
 :io => $stdout.dup,
}
# since tapout uses a unix pipe, we don't want any buffering
options[:io].sync = true
$stdout.reopen($stderr)

tests = OptionParser.new do |opts|
  opts.on "--help", "Display this help." do
    puts opts
    exit
  end

  desc = "Sets random seed. Also via env. Eg: SEED=n rake"
  opts.on "--seed SEED", Integer, desc do |m|
    options[:seed] = m.to_i
  end

  opts.on "--verbose", "Verbose. Show progress processing files." do
    options[:verbose] = true
  end

  opts.on "--name PATTERN", "Filter run on /regexp/ or string." do |a|
    options[:filter] = a
  end

  opts.on "--dry-run" do
    Minitest.instance_eval do
      class <<self
        remove_method :run_one_method
        def run_one_method(klass, method_name)
          klass.new(method_name)
        end
      end
    end
  end
end.parse(ARGV)

unless options[:seed] then
  srand
  options[:seed] = (ENV["SEED"] || srand).to_i % 0xFFFF
end

srand options[:seed]

reporter = TapJRunner.new(options)
$stdout.reopen($stderr)

tests.each { |test| load(test) }

Minitest.reporter = nil # runnables shouldn't depend on the reporter, ever
reporter.start
Minitest.__run(reporter, options)
reporter.report
