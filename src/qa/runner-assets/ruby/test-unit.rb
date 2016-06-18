gem 'test-unit'
require 'test-unit'

require 'test/unit/ui/testrunner'
require 'test/unit/ui/testrunnermediator'
require 'stringio'

require 'json'

module Test::Unit::UI::Tap
  class TapjTestRunner < Test::Unit::UI::TestRunner
    REVISION = 4

    def initialize(suite, options={})
      super

      @output = @options[:output]
      @seed = @options[:seed]
      @trace = @options[:trace]
      @level = 0
      @already_outputted = false
      @top_level = true
      @counts = Hash.new{ |h,k| h[k] = 0 }
      @stdcom = ::Qa::Stdcom.new
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

      emit(
          'type'  => 'suite',
          'start' => ::Qa::Time.strftime(::Qa::Time.now_f, '%Y-%m-%d %H:%M:%S'),
          'count' => @suite.size,
          'seed'  => @seed,
          'rev'   => REVISION)
    end

    def tapout_after_suite(elapsed_time)
      emit(
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

      emit doc
    end

    #
    # After each case, decrement the case level.
    #
    def tapout_after_case(testcase)
      @level -= 1
    end

    #
    def tapout_before_test(test)
      @test_start = ::Qa::Time.now_f
      @test = test
      # set up stdout and stderr to be captured
      @stdcom.reset!
    end

    #
    def tapout_fault(fault)
      @counts[:total] += 1

      doc = {
        'type'        => 'test',
        'label'       => clean_label(fault.test_name),
        'filter'      => "#{@test.class.name}##{fault.method_name}",
        'file'        => @test && @test.method(fault.method_name).source_location[0], # returns [file, line]
        'time'        => ::Qa::Time.now_f - @test_start
      }
      case fault
      when Test::Unit::Pending
        @counts[:todo]  += 1

        doc.merge!(
            'status'      => 'todo',
            'exception'   => ::Qa::TapjExceptions.summarize_exception(fault, fault.location))

      when Test::Unit::Omission
        @counts[:todo]  += 1

        doc.merge!(
            'status'      => 'todo',
            'exception'   => ::Qa::TapjExceptions.summarize_exception(fault, fault.location))
      when Test::Unit::Notification
        doc.merge!(
            'text' => note.message)
      when Test::Unit::Failure
        @counts[:fail]  += 1

        doc.merge!(
            'status'      => 'fail',
            'expected'    => fault.inspected_expected,
            'returned'    => fault.inspected_actual,
            'exception'   => ::Qa::TapjExceptions.summarize_exception(fault, fault.location, fault.user_message))
      else
        @counts[:error] += 1

        doc.merge!(
            'status'      => 'error',
            'exception'   => ::Qa::TapjExceptions.summarize_exception(fault.exception, fault.location))
      end

      @stdcom.drain!(doc)


      @trace.emit_stats
      emit doc
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
        'filter'      => "#{@test.class.name}##{test.method_name}",
        'file'        => @test && @test.method(test.method_name).source_location[0], # returns [file, line]
        'time'        => ::Qa::Time.now_f - @test_start
      }
      @stdcom.drain!(doc)

      @trace.emit_stats
      emit doc
    end

    #
    def clean_label(name)
      name.sub(/\(.+?\)\z/, '').chomp('()')
    end

    def emit(doc)
      @output.emit(doc)
    end
  end
end


engine = ::Qa::TestEngine.new
engine.def_prefork do |files|
  Test::Unit::AutoRunner.register_runner(:tapj) do |auto_runner|
    Test::Unit::UI::Tap::TapjTestRunner
  end

  files.each do |file|
    load(file)
  end
end

engine.def_run_tests do |qa_trace, opt, tapj_conduit, tests|
  if opt.dry_run
    Test::Unit::TestCase.class_eval do
      remove_method :run_test
      def run_test; end
      def run_setup; end
      def run_teardown; end
      def run_cleanup; end
    end
  end

  seed = opt.seed % 0xFFFF
  srand(seed)

  auto_runner = Test::Unit::AutoRunner.new(false)
  auto_runner.prepare
  args = ['--runner', 'tapj']
  unless tests.empty?
    auto_runner.filters.push(lambda do |test|
      key = "#{test.class.name}##{test.method_name}"
      tests.member?(key)
    end)
  end
  auto_runner.process_args(args)
  runner_options = auto_runner.runner_options
  runner_options[:output] = tapj_conduit
  runner_options[:seed] = seed
  runner_options[:trace] = qa_trace

  auto_runner.run
end

engine.main(ARGV)

# Explicitly exit to avoid autorun logic.
exit(0)
