require 'json'

require 'rspec/core'
require 'rspec/core/formatters/base_formatter'

module RSpec
  class TapjFormatter < Core::Formatters::BaseFormatter

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

    # TAP-Y/J Revision
    REVISION = 4

    attr_accessor :example_group_stack
    attr_reader :summary

    #
    def initialize(output)
      super(output)
      @summary = nil
      @example_group_stack = []
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

      now = Time.now

      doc = {
        'type'  => 'suite',
        'start' => now.strftime('%Y-%m-%d %H:%M:%S'),
        'count' => notification.count,
        'seed'  => @seed,
        'rev'   => REVISION
      }
      puts_json doc
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
      @example_group_stack << example_group
      puts_json doc
    end

    # This method is invoked at the end of the execution of each example group.
    # +example_group+ is the example_group.
    def example_group_finished(notification)
      @example_group_stack.pop
    end

    # Set up stdout and stderr to be captured.
    #
    # IMPORTANT: Comment out the `reset_output` line to debug!!!!!!!!!
    #
    def example_started(notification)
      @start_time = Time.now
      reset_output
    end

    def example_base(notification, status)
      example = notification.example

      file, line = example.location.split(':')
      file = relative_path(file)
      line = line.to_i

      doc = {
        'type'     => 'test',
        'subtype'  => 'it',
        'status'   => status,
        'filter'   => example.location,
        'label'    => "#{example.description}",
        'file'     => file,
        'line'     => line,
        'source'   => ::TapjExceptions.source(file)[line-1].strip,
        'snippet'  => ::TapjExceptions.code_snippet(file, line),
        'time' => Time.now - @start_time
      }
      doc.update(captured_output)

      doc
    end

    #
    def example_passed(notification)
      puts_json example_base(notification, 'pass')
    end

    #
    def example_pending(notification)
      puts_json example_base(notification, 'todo')
    end

    #
    def example_failed(notification)
      example = notification.example
      exception = example.exception
      doc = example_base(
          notification,
          RSpec::Expectations::ExpectationNotMetError === exception ? 'fail' : 'error')

      if doc['status'] == 'fail'
        if md = /expected:\s*(.*?)\n\s*got:\s*(.*?)\s+/.match(exception.to_s)
          expected, returned = md[1], md[2]
          doc.update(
              'expected' => expected,
              'returned' => returned)
        end
      end

      doc.update(
          'exception' => ::TapjExceptions.summarize_exception(
              exception,
              format_backtrace(exception.backtrace, example.metadata)))

      puts_json doc
    end

    # This method is invoked after the dumping of examples and failures.
    def dump_summary(summary_notification)
      duration      = summary_notification.duration
      example_count = summary_notification.examples.size
      failure_count = summary_notification.failed_examples.size
      pending_count = summary_notification.pending_examples.size

      failed_examples = summary_notification.failed_examples

      error_count = 0

      failed_examples.each do |e|
        if RSpec::Expectations::ExpectationNotMetError === e.exception
          #failure_count += 1
        else
          failure_count -= 1
          error_count += 1
        end
      end

      passing_count = example_count - failure_count - error_count - pending_count

      @summary = {
        'type' => 'final',
        'time' => duration,
        'counts' => {
          'total' => example_count,
          'pass'  => passing_count,
          'fail'  => failure_count,
          'error' => error_count,
          'omit'  => 0,
          'todo'  => pending_count
        }
      }

      puts_json @summary
    end

    def seed(notification)
      @seed = notification.seed
    end

    # Add any messages as notes.
    def message(message_notification)
      puts_json('type' => 'note', 'text' => message_notification.message)
    end

    def passed?
      counts = @summary['counts']
      (counts['fail'] + counts['error']).zero?
    end

  private

    def puts_json(doc)
      output.write "#{doc.to_json}\n"
      output.flush
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
      return unless (@_newout && @_newerr)

      stdout = @_newout.string.chomp("\n")
      stderr = @_newerr.string.chomp("\n")

      doc = {}
      doc['stdout'] = stdout unless stdout.empty?
      doc['stderr'] = stderr unless stderr.empty?

      $stdout = @_oldout
      $stderr = @_olderr

      return doc
    end

    #
    def capture_io
      ostdout, ostderr = $stdout, $stderr
      cstdout, cstderr = StringIO.new, StringIO.new
      $stdout, $stderr = cstdout, cstderr

      yield

      return cstdout.string.chomp("\n"), cstderr.string.chomp("\n")
    ensure
      $stdout = ostdout
      $stderr = ostderr
    end

    #
    def relative_path_regex
      @relative_path_regex ||= /(\A|\s)#{File.expand_path('.')}(#{File::SEPARATOR}|\s|\Z)/
    end

    # Get relative path of file.
    #
    # line - current code line [String]
    #
    # Returns relative path to line. [String]
    def relative_path(line)
      line = line.sub(relative_path_regex, "\\1.\\2".freeze)
      line = line.sub(/\A([^:]+:\d+)$/, '\\1'.freeze)
      return nil if line == '-e:1'.freeze
      line
    rescue SecurityError
      nil
    end

    #
    def format_backtrace(*args)
      backtrace_formatter.format_backtrace(*args)
    end

    #
    def backtrace_formatter
      RSpec.configuration.backtrace_formatter
    end
  end
end

config = RSpec.configuration
config.output_stream = $stderr

formatter = RSpec::TapjFormatter.new($stdout.dup)
$stdout.reopen($stderr)

# create reporter with json formatter
reporter =  RSpec::Core::Reporter.new(config)
config.instance_variable_set(:@reporter, reporter)

# internal hack
# api may not be stable, make sure lock down Rspec version
loader = config.send(:formatter_loader)
notifications = loader.send(:notifications_for, RSpec::TapjFormatter)

reporter.register_listener(formatter, *notifications)

RSpec::Core::Runner.run(ARGV)
