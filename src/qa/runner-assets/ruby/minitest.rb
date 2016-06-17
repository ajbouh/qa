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
    @io      = options.delete(:io)
    @trace   = options.delete(:trace)
    @options = options

    @assertions = 0
    @count      = 0
    @results = []
    @suite_start_time = nil
    @test_count = nil

    @previous_case = nil
    @stdcom = ::Qa::Stdcom.new
  end

  #
  # Minitest's initial hook ran just before testing begins.
  #
  def start
    @suite_start_time = ::Qa::Time.now_f

    @test_cases = Minitest::Runnable.runnables
    count_tests!(@test_cases)

    @stdcom.reset!

    emit(
        'type'  => 'suite',
        'start' => ::Qa::Time.strftime(@suite_start_time, '%Y-%m-%d %H:%M:%S'),
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
      emit(
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

    @stdcom.drain!(doc)

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
      doc['exception'] = ::Qa::TapjExceptions.summarize_exception(
          exception.error, exception.backtrace)
    end

    @trace.emit_stats
    emit(doc)

    @previous_case = result.class
  end

  #
  # Minitest's finalization hook.
  #
  def report
    aggregate = @results.group_by { |r| r.failure.class }
    aggregate.default = [] # dumb. group_by should provide this

    corrected_time = ::Qa::Time.now_f - @suite_start_time
    emit(
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

  def emit(obj)
    @io.emit(obj)
    @io.flush
  end

  def count_tests!(test_cases)
    filter = @options[:filter] || '/./'
    filter = Regexp.new $1 if filter =~ /\/(.*)\//

    @test_count = test_cases.inject(0) do |acc, test_case|
      acc + test_case.runnable_methods.grep(filter).length
    end
  end
end

engine = ::Qa::TestEngine.new
engine.def_prefork do |files|
  files.each do |file|
    load(file)
  end
end

engine.def_run_tests do |qa_trace, opt, tapj_conduit, tests|
  if opt.dry_run
    Minitest.instance_eval do
      class <<self
        remove_method :run_one_method
        def run_one_method(klass, method_name)
          klass.new(method_name)
        end
      end
    end
  end

  options = {
    seed: opt.seed,
    io: tapj_conduit,
    trace: qa_trace,
    filter: tests.empty? ? nil : "/^(?:#{tests.map{|test|Regexp.escape(test)} * '|'})$/",
  }

  srand(options[:seed] % 0xFFFF)

  reporter = TapJRunner.new(options)

  Minitest.reporter = nil # runnables shouldn't depend on the reporter, ever
  reporter.start
  Minitest.__run(reporter, options)
  reporter.report
end

engine.main(ARGV)

# Explicitly exit to avoid Minitest autorun logic.
exit(0)
