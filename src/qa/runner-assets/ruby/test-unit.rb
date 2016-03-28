gem 'test-unit'
require 'test-unit'

require 'test/unit/ui/testrunner'
require 'test/unit/ui/testrunnermediator'
require 'stringio'

require 'json'

$__qa_secret_stdout = $stdout.dup

module Test::Unit::UI::Tap
  class TapjTestRunner < Test::Unit::UI::TestRunner
    REVISION = 4

    def initialize(suite, options={})
      super

      @output = @options[:output] || $__qa_secret_stdout
      @level = 0
      @already_outputted = false
      @top_level = true
      @counts = Hash.new{ |h,k| h[k] = 0 }
    end

  private
    def attach_to_mediator
      @mediator.add_listener(Test::Unit::TestResult::FAULT,                &method(:tapout_fault))
      @mediator.add_listener(Test::Unit::UI::TestRunnerMediator::STARTED,  &method(:tapout_before_suite))
      @mediator.add_listener(Test::Unit::UI::TestRunnerMediator::FINISHED, &method(:tapout_after_suite))
      @mediator.add_listener(Test::Unit::TestCase::STARTED_OBJECT,         &method(:tapout_before_test))
      @mediator.add_listener(Test::Unit::TestCase::FINISHED_OBJECT,        &method(:tapout_pass))
      @mediator.add_listener(Test::Unit::TestSuite::STARTED_OBJECT,        &method(:tapout_before_case))
      @mediator.add_listener(Test::Unit::TestSuite::FINISHED_OBJECT,       &method(:tapout_after_case))
    end

    def tapout_before_suite(result)
      @result = result

      puts_json(
          'type'  => 'suite',
          'start' => Time.now.strftime('%Y-%m-%d %H:%M:%S'),
          'count' => @suite.size,
          'seed'  => $__qa_seed,
          'rev'   => REVISION)
    end

    def tapout_after_suite(elapsed_time)
      puts_json(
          'type' => 'final',
          'time' => elapsed_time,
          'counts' => {
            'total' => @counts[:total],
            'pass'  => @counts[:pass],
            'fail'  => @counts[:fail],
            'error' => @counts[:error],
            'omit'  => @counts[:omit],
            'todo'  => @counts[:todo],
          })
    end

    def tapout_before_case(testcase)
      return nil if testcase.test_case.nil?

      @test_case = testcase

      doc = {
        'type'    => 'case',
        'label'   => testcase.name,
        'level'   => @level
      }

      @level += 1

      puts_json doc
    end

    #
    # After each case, decrement the case level.
    #
    def tapout_after_case(testcase)
      @level -= 1
    end

    #
    def tapout_before_test(test)
      @test_start = Time.now
      @test = test
      # set up stdout and stderr to be captured
      reset_output
    end

    #
    def tapout_fault(fault)
      @counts[:total] += 1

      doc = {
        'type'        => 'test',
        'label'       => clean_label(fault.test_name),
        'filter'      => fault.method_name,
        'file'        => @test.method(fault.method_name).source_location[0], # returns [file, line]
        'time'        => Time.now - @test_start
      }
      case fault
      when Test::Unit::Pending
        @counts[:todo]  += 1

        doc.merge!(
            'status'      => 'todo',
            'exception'   => TapjExceptions.summarize_exception(fault, fault.location))

      when Test::Unit::Omission
        @counts[:todo]  += 1

        doc.merge!(
            'status'      => 'todo',
            'exception'   => TapjExceptions.summarize_exception(fault, fault.location))
      when Test::Unit::Notification
        doc.merge!(
            'text' => note.message)
      when Test::Unit::Failure
        @counts[:fail]  += 1

        doc.merge!(
            'status'      => 'fail',
            'expected'    => fault.inspected_expected,
            'returned'    => fault.inspected_actual,
            'exception'   => TapjExceptions.summarize_exception(fault, fault.location, fault.user_message))
      else
        @counts[:error] += 1

        doc.merge!(
            'status'      => 'error',
            'exception'   => TapjExceptions.summarize_exception(fault, fault.location))
      end

      puts_json doc
      @already_outputted = true
    end

    #
    def tapout_pass(test)
      if @already_outputted
        @already_outputted = false
        return nil
      end

      @counts[:total] += 1
      @counts[:pass]  += 1

      doc = {
        'type'        => 'test',
        'status'      => 'pass',
        'label'       => clean_label(test.name),
        'filter'      => test.method_name,
        'file'        => test.method(test.method_name).source_location[0], # returns [file, line]
        'time'        => Time.now - @test_start
      }

      puts_json doc
    end

    #
    def clean_label(name)
      name.sub(/\(.+?\)\z/, '').chomp('()')
    end

    def puts_json(doc)
      @output.write(doc.to_json.chomp+"\n")
      @output.flush
    end

    #
    def reset_output
      @_oldout = $stdout
      @_olderr = $stderr

      @_newout = StringIO.new
      @_newerr = StringIO.new

      $stdout = @_newout
      $stderr = @_newerr
    end

    #
    def captured_output
      stdout = @_newout.string.chomp("\n")
      stderr = @_newerr.string.chomp("\n")

      doc = {}
      doc['stdout'] = stdout unless stdout.empty?
      doc['stderr'] = stderr unless stderr.empty?

      $stdout = @_oldout
      $stderr = @_olderr

      return doc
    end
  end
end

Test::Unit::AutoRunner.register_runner(:tapj) do |auto_runner|
  Test::Unit::UI::Tap::TapjTestRunner
end

$stdout.reopen($stderr)

if ARGV.delete('--dry-run')
  Test::Unit::TestCase.class_eval do
    remove_method :run_test
    def run_test; end
    def run_setup; end
    def run_teardown; end
    def run_cleanup; end
  end
end

if seed_ix = ARGV.index('--seed')
  ARGV.delete_at(seed_ix)
  $__qa_seed = ARGV.delete_at(seed_ix).to_i % 0xFFFF
  srand($__qa_seed)
end

auto_runner = Test::Unit::AutoRunner.new(false)
auto_runner.prepare
args = ['--runner', 'tapj', *ARGV]
auto_runner.process_args(args)

args.each { |t| load(t) }

auto_runner.run
