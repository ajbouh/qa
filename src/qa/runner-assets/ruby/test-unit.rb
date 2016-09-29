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
      @output.emit_suite_event(::Qa::Time.now_f, @suite.size, @seed)
    end

    def tapout_after_suite(elapsed_time)
      @trace.emit_final_stats
      @output.emit_final_event(elapsed_time)
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
      @already_outputted = false

      @output.emit_test_begin_event(
          @test_start,
          'test',
          clean_label(test.name),
          "#{@test.class.name}##{test.method_name}")

      # set up stdout and stderr to be captured
      @stdcom.reset!
    end

    #
    def tapout_fault(fault)
      doc = {
        'type'        => 'test',
        'runner'      => 'test-unit',
        'label'       => clean_label(fault.test_name),
        'filter'      => "#{@test.class.name}##{fault.method_name}",
        'file'        => @test && @test.method(fault.method_name).source_location[0], # returns [file, line]
        'time'        => ::Qa::Time.now_f - @test_start
      }

      exception = nil
      exception_location = nil
      exception_message = nil
      case fault
      when Test::Unit::Pending
        exception, exception_location = fault, fault.location
        doc['status'] = 'todo'
      when Test::Unit::Omission
        exception, exception_location = fault, fault.location
        doc['status'] = 'todo'
      when Test::Unit::Notification
        doc['text'] = note.message
      when Test::Unit::Failure
        exception, exception_location, exception_message = fault, fault.location, fault.user_message
        doc.merge!(
            'status'      => 'fail',
            'expected'    => fault.inspected_expected,
            'returned'    => fault.inspected_actual)
      else
        exception, exception_location = fault.exception, fault.location
        doc['status'] = 'error'
      end

      if exception
        doc['exception'] = ::Qa::TapjExceptions.summarize_exception(exception, exception_location)
      end

      @stdcom.drain!(doc)

      if exception
        ::Qa::TapjExceptions.maybe_emit_and_await_attach(nil, exception, doc)
      end

      @trace.emit_stats
      emit doc

      @already_outputted = true

      nil
    end

    #
    def tapout_pass(test)
      if @already_outputted
        return nil
      end

      doc = {
        'type'        => 'test',
        'runner'      => 'test-unit',
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
      @output.flush
    end
  end
end


engine = ::Qa::TestEngine.new
engine.def_prefork do
  Test::Unit::AutoRunner.register_runner(:tapj) do |auto_runner|
    Test::Unit::UI::Tap::TapjTestRunner
  end
end

engine.def_run_tests do |qa_trace, opt, tapj_conduit, tests|
  if opt.dry_run
    Test::Unit::TestCase.class_eval do
      remove_method :run_test
      def run_test; end

      def run_setup
        yield if block_given?
      end

      def run_cleanup; end
      def run_teardown; end
    end

    if defined?(::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit)
      # Modified from https://github.com/rails/rails/blob/3-2-stable/activesupport/lib/active_support/testing/setup_and_teardown.rb#L61
      ::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit.module_eval do
        remove_method :run

        # This redefinition is unfortunate but test/unit shows us no alternative.
        # Doubly unfortunate: hax to support Mocha's hax.
        def run(result)
          return if @method_name.to_s == "default_test"

          @_result = result
          @internal_data.test_started

          yield(Test::Unit::TestCase::STARTED, name)
          yield(Test::Unit::TestCase::STARTED_OBJECT, self)

          @internal_data.test_finished
          result.add_run
          yield(Test::Unit::TestCase::FINISHED, name)
          yield(Test::Unit::TestCase::FINISHED_OBJECT, self)
        end
      end
    end
  else
    if defined?(::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit)
      # Modified from https://github.com/rails/rails/blob/3-2-stable/activesupport/lib/active_support/testing/setup_and_teardown.rb#L61
      ::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit.module_eval do
        remove_method :run

        # This redefinition is unfortunate but test/unit shows us no alternative.
        # Doubly unfortunate: hax to support Mocha's hax.
        def run(result)
          return if @method_name.to_s == "default_test"

          @_result = result
          @internal_data.test_started

          mocha_counter = retrieve_mocha_counter(self, result)
          yield(Test::Unit::TestCase::STARTED, name)
          yield(Test::Unit::TestCase::STARTED_OBJECT, self)

          begin
            begin
              run_callbacks :setup do
                setup
                __send__(@method_name)
                mocha_verify(mocha_counter) if mocha_counter
              end
            rescue Mocha::ExpectationError => e
              add_failure(e.message, e.backtrace)
            rescue Test::Unit::AssertionFailedError => e
              add_failure(e.message, e.backtrace)
            rescue Exception => e
              raise if ::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit::PASSTHROUGH_EXCEPTIONS.include?(e.class)
              add_error(e)
            ensure
              begin
                teardown
                run_callbacks :teardown
              rescue Mocha::ExpectationError => e
                add_failure(e.message, e.backtrace)
              rescue Test::Unit::AssertionFailedError => e
                add_failure(e.message, e.backtrace)
              rescue Exception => e
                raise if ::ActiveSupport::Testing::SetupAndTeardown::ForClassicTestUnit::PASSTHROUGH_EXCEPTIONS.include?(e.class)
                add_error(e)
              end
            end
          ensure
            mocha_teardown if mocha_counter
          end

          result.add_run
          @internal_data.test_finished

          yield(Test::Unit::TestCase::FINISHED, name)
          yield(Test::Unit::TestCase::FINISHED_OBJECT, self)
        end
      end
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
