ENV['CPUPROFILE_FREQUENCY'] = '51'

require 'thread'

require 'rspec/core'
require 'rspec/core/formatters/base_formatter'

module ::Qa::Rspec
  def self.filter_for_example(example, ix)
    descriptions = [ix]
    ::RSpec::Core::Metadata.ascending(example.metadata) do |meta|
      descriptions.push([meta[:description], meta[:location]])
    end

    descriptions_json = JSON.generate(descriptions)

    return "#{example.metadata[:location]}:#{Digest::SHA2.hexdigest(descriptions_json)[0...8]}"
  end

  #
  def self.relative_path_regex
    @relative_path_regex ||= /(\A|\s)#{File.expand_path('.')}(#{File::SEPARATOR}|\s|\Z)/
  end

  # Get relative path of file.
  #
  # line - current code line [String]
  #
  # Returns relative path to line. [String]
  def self.relative_path(line)
    line = line.sub(relative_path_regex, "\\1.\\2".freeze)
    line = line.sub(/\A([^:]+:\d+)$/, '\\1'.freeze)
    return nil if line == '-e:1'.freeze
    line
  rescue SecurityError
    nil
  end

  def self.example_base(example, status=nil)
    exception = example.exception

    unless status
      if exception
        status = ::RSpec::Expectations::ExpectationNotMetError === exception ? 'fail' : 'error'
      else
        status = 'pass'
      end
    end

    file, line = example.location.split(':')
    file = relative_path(file)
    line = line.to_i

    doc = {
      'type'     => 'test',
      'subtype'  => 'it',
      'runner'   => 'rspec',
      'status'   => status,
      'filter'   => example.metadata[:qa_filter],
      'label'    => "#{example.description}",
      'file'     => file,
      'line'     => line,
      'time'     => example.execution_result.run_time
    }

    if status == 'todo'
      doc['exception'] = {
        'message' => example.execution_result.pending_message
      }
    elsif exception
      doc['exception'] = ::Qa::TapjExceptions.summarize_exception(
          exception, exception.backtrace || [])
    end

    doc
  rescue
    $__qa_stderr.puts([$!, $@].join("\n\t"))
  end

  class TapjFormatter < ::RSpec::Core::Formatters::BaseFormatter
    ::RSpec::Core::Formatters.register self,
        :start,
        :example_group_started,
        :example_group_finished,
        :example_started,
        :example_passed,
        :example_failed,
        :example_pending,
        :dump_summary,
        :seed,
        :message,
        :close

    attr_accessor :example_group_stack
    attr_reader :summary

    #
    def initialize(trace, output)
      super(output)
      @trace = trace
      @summary = nil
      @example_group_stack = []
      @example_count = 0
      @example_count_stack = []
      @stdcom = ::Qa::Stdcom.new
    end

    #
    # This method is invoked before any examples are run, right after
    # they have all been collected. This can be useful for special
    # formatters that need to provide progress on feedback (graphical ones)
    #
    # This will only be invoked once, and the next one to be invoked
    # is #example_group_started
    #
    def start(notification)
      # there is a super method for this
      super(notification)

      now = ::Qa::Time.now_f
      output.emit_suite_event(now, notification.count, @seed)
    end

    #
    # This method is invoked at the beginning of the execution of each example group.
    # +example_group+ is the example_group.
    #
    # The next method to be invoked after this is +example_passed+,
    # +example_pending+, or +example_finished+
    #
    def example_group_started(notification)
      super(notification)
      doc = {
        'type'    => 'case',
        'subtype' => 'describe',
        'label'   => "#{notification.group.description}",
        'level'   => @example_group_stack.size
      }
      output.emit(doc)
      @trace.emit_begin('rspec describe', 'args' => doc)

      @example_count_stack << @example_count
      @example_group_stack << example_group
    end

    # This method is invoked at the end of the execution of each example group.
    # +example_group+ is the example_group.
    def example_group_finished(notification)
      previous_example_count = @example_count_stack.pop
      group_example_count = @example_count - previous_example_count
      @example_group_stack.pop

      @trace.emit_end('rspec describe',
          'args' => {'example_count' => group_example_count})
    end

    def example_started(notification)
      example = notification.example

      # We're fudging this a little, since the example's execution_result
      # doesn't have a started_at time set until after all reporters return
      # from this method.
      start_time = ::Qa::Time.now_f
      @trace.emit_begin('rspec it', 'ts' => start_time * 1e6)

      output.emit_test_begin_event(
          start_time,
          example.description,
          'it',
          example.metadata[:qa_filter],
          ::Qa::Rspec.relative_path(example.location.split(':')[0]))

      @stdcom.reset!
    end

    #
    def example_passed(notification)
      @example_count += 1
      @trace.emit_stats
      output.emit(example_base(notification.example, 'pass'))
      output.flush

      nil
    end

    #
    def example_pending(notification)
      @example_count += 1
      @trace.emit_stats
      output.emit(example_base(notification.example, 'todo'))
      output.flush

      nil
    end

    #
    def example_failed(notification)
      @example_count += 1
      output.emit(example_base(notification.example))
      output.flush

      nil
    end

    # This method is invoked after the dumping of examples and failures.
    def dump_summary(summary_notification)
      @trace.emit_final_stats
      output.emit_final_event(summary_notification.duration)
    end

    def seed(notification)
      @seed = notification.seed
    end

    # Add any messages as notes.
    def message(message_notification)
      output.emit('type' => 'note', 'text' => message_notification.message)
    end

    def passed?
      output.passed?
    end

  private

    def example_base(example, status=nil)
      doc = ::Qa::Rspec.example_base(example, status)

      @stdcom.drain!(doc)
      @trace.emit_end('rspec it',
          'ts' => example.execution_result.finished_at.to_f * 1e6,
          'args' => doc)

      doc
    end
  end
end


engine = ::Qa::TestEngine.new
groups = nil
engine.def_prefork do
  # Trigger eager loading.
  ::Qa::EagerLoad::Autoload.load_constants_recursively(RSpec)

  groups = RSpec.world.ordered_example_groups.dup
  groups.each do |group|
    group.descendants.each do |g|
      g.examples.each_with_index do |example, ix|
        example.metadata[:qa_filter] = ::Qa::Rspec.filter_for_example(example, ix)
      end
    end
  end

  ::RSpec.clear_examples
end

module ::Qa::Rspec::MaybeAttachDebuggerBeforeAfterHook
  def run_after_example
    if self.exception
      doc = ::Qa::Rspec.example_base(self)
      ::Qa::TapjExceptions.maybe_emit_and_await_attach(self, self.exception, doc)
    end

    super
  end
end

::RSpec::Core::Example.class_eval do
  prepend ::Qa::Rspec::MaybeAttachDebuggerBeforeAfterHook
end

engine.def_run_tests do |qa_trace, opt, tapj_conduit, tests|
  world = ::RSpec.world
  rspec_config = ::RSpec.configuration

  unless tests.empty?
    world.filter_manager.include(:qa_filter => lambda { |v| tests.include?(v) })
  end

  formatter = ::Qa::Rspec::TapjFormatter.new(qa_trace, tapj_conduit)
  # create reporter with json formatter
  reporter = ::RSpec::Core::Reporter.new(rspec_config)
  # internal hack
  # api may not be stable, make sure lock down Rspec version
  loader = rspec_config.send(:formatter_loader)
  notifications = loader.send(:notifications_for, ::Qa::Rspec::TapjFormatter)
  reporter.register_listener(formatter, *notifications)

  if opt.dry_run
    rspec_config.dry_run = true
  end

  reporter.report(world.example_count(groups)) do |reporter|
    rspec_config.with_suite_hooks do
      groups.each do |g|
        g.run(reporter)
      end
    end
  end
end

engine.main(ARGV)
