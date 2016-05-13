ENV['CPUPROFILE_FREQUENCY'] = '51'

$__qa_stderr = $stderr.dup

trace_probes = []
while trace_probe_ix = ARGV.index('--trace-probe')
  ARGV.delete_at(trace_probe_ix)
  trace_probes.push(ARGV.delete_at(trace_probe_ix))
end

require 'thread'

trace_events = []
qa_trace = ::Qa::Trace.new(Process.pid) { |e| trace_events.push(e) }
trace_probes.each { |trace_probe| qa_trace.define_tracer(trace_probe) }
qa_trace.start

qa_trace.emit_dur('require rspec') do
  require 'rspec/core'
  require 'rspec/core/formatters/base_formatter'
end

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
    def initialize(trace, output)
      super(output)
      @trace = trace
      @summary = nil
      @example_group_stack = []
      @example_count = 0
      @example_count_stack = []
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

      emit({
            'type'  => 'suite',
            'start' => ::Qa::Time.strftime(now, '%Y-%m-%d %H:%M:%S'),
            'count' => notification.count,
            'seed'  => @seed,
            'rev'   => REVISION
          })
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
      emit(doc)
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

    # Set up stdout and stderr to be captured.
    #
    # IMPORTANT: Comment out the `reset_output` line to debug!!!!!!!!!
    #
    def example_started(notification)
      @start_time = ::Qa::Time.now_f
      @trace.emit_begin('rspec it', 'ts' => @start_time * 1e6)

      example = notification.example
      emit(
          'type' => 'note',
          'qa:type' => 'test:begin',
          'qa:timestamp' => @start_time,
          'qa:label' => "#{example.description}",
          'qa:subtype' => 'it',
          'qa:filter' => example.object_id.to_s)

      reset_output
    end

    def example_base(notification, status)
      example = notification.example

      file, line = example.location.split(':')
      file = relative_path(file)
      line = line.to_i

      now = ::Qa::Time.now_f
      time = now - @start_time
      doc = {
        'type'     => 'test',
        'subtype'  => 'it',
        'status'   => status,
        'filter'   => example.object_id.to_s,
        'label'    => "#{example.description}",
        'file'     => file,
        'line'     => line,
        'source'   => (::Qa::TapjExceptions.source(file)[line-1] || '').strip,
        'snippet'  => ::Qa::TapjExceptions.code_snippet(file, line),
        'time' => time
      }
      doc.update(captured_output)

      @trace.emit_end('rspec it',
          'ts' => now * 1e6,
          'args' => doc)

      doc
    end

    #
    def example_passed(notification)
      @example_count += 1
      emit example_base(notification, 'pass')
    end

    #
    def example_pending(notification)
      @example_count += 1
      emit example_base(notification, 'todo')
    end

    #
    def example_failed(notification)
      @example_count += 1
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
          'exception' => ::Qa::TapjExceptions.summarize_exception(
              exception,
              format_backtrace(exception.backtrace, example.metadata)))

      emit doc
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

      @trace.emit_final_stats

      emit @summary
    end

    def seed(notification)
      @seed = notification.seed
    end

    # Add any messages as notes.
    def message(message_notification)
      emit('type' => 'note', 'text' => message_notification.message)
    end

    def passed?
      counts = @summary['counts']
      (counts['fail'] + counts['error']).zero?
    end

  private

    def emit(doc)
      @trace.emit_stats
      output.emit(doc)
      output.flush
    end

    def start_read_thread_pipe_replacing(io)
      tempfile = Tempfile.new('stdio')

      io.reopen(tempfile.path)
      return tempfile
    end

    def drain_read_thread_pipe(wr, tempfile)
      wr.flush
      tempfile.read
    ensure
      tempfile.close!
    end

    #
    def reset_output
      @_newout_f = start_read_thread_pipe_replacing($stdout)
      @_newerr_f = start_read_thread_pipe_replacing($stderr)
    end

    #
    def captured_output
      doc = {}

      return doc unless (@_newout_f && @_newerr_f)

      stdout = drain_read_thread_pipe($stdout, @_newout_f).chomp("\n")
      stderr = drain_read_thread_pipe($stderr, @_newerr_f).chomp("\n")

      doc['stdout'] = stdout unless stdout.empty?
      doc['stderr'] = stderr unless stderr.empty?

      return doc
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

module Client
  module_function

  require 'socket'
  def connect(address)
    if /^(.*)@([^:]+):(\d+)$/ =~ address
      token, ip, port = $1, $2, $3
      socket = TCPSocket.new(ip, port)
      socket.puts token
      socket.flush
      socket
    elsif /^(.*)@([^:]+)$/ =~ address
      token, unix = $1, $2, $3
      socket = UNIXSocket.new(unix)
      socket.puts token
      socket.flush
      socket
    else
      abort("Malformed address: #{address}")
    end
  end
end

socket = nil
qa_trace.emit_dur('connect to socket') do
  socket = Client.connect(ARGV.shift)
end

# class Thread
#   class << self
#     alias :__qa_original_new :new
#     def new(&b)
#       $__qa_trace.emit_dur('Thread.new', 'backtrace' => caller) do |h|
#         thr = __qa_original_new(&b)
#         h['spawnedTid'] = thr.object_id
#         thr
#       end
#     end
#
#     alias :start :new
#   end
# end

require 'rails_helper'
require 'erb'

ARGV.each { |arg| load arg }

module X
  module_function
  def load_constants_recursively(mod)
    visited = Set.new
    soon = [mod]

    while m = soon.pop
      visited.add(m)
      m.constants(false).each do |c|
        # next unless m.autoload?(c)

        begin
          val = m.const_get(c)
          next unless (val.is_a?(Module) || val.is_a?(Class)) && !visited.member?(val)
          soon.push(val)
        rescue LoadError
        end
      end
    end
  end
end

# Trigger eager loading. Should do this automatically...
X.load_constants_recursively(RSpec)

if defined?(ActionController)
  require 'action_controller/metal/testing'
  # ActionController::TestCase
  # ActionController::Metal::Testing
end

if defined?(Rack)
  X.load_constants_recursively(Rack)
end

if defined?(I18n)
  I18n.fallbacks[I18n.locale] if I18n.respond_to? :fallbacks
  I18n.default_locale
end


# HACK(adamb) Needs to be done at the example group level, so we can exploit reuse of
# the controller.
# if defined?(::ApplicationController)
#   ::ApplicationController.descendants.each do |ac|
#     c = ac.new
#     c.action_methods
#     c.view_context
#     c.view_renderer.send(:_template_renderer)
#     c.view_renderer.send(:_partial_renderer)
#   end
# end


if defined?(Rails)
  # Do this earlier, so we can avoid lazy requires and things like
  # ActiveRecord::Base.descendants is fully populated.
  Rails.application.eager_load!

  # Eager load this (it does an internal require)
  Rails.backtrace_cleaner

  # Eager load application routes.
  Rails.application.routes.routes
end

$__qa_lazily_loaded_constants = []
# Track lazily loaded values that could have been eager loaded.
# if defined?(ActiveSupport::Dependencies)
#   module ActiveSupport::Dependencies
#     alias :__qa_original_load_missing_constant :load_missing_constant
#     def load_missing_constant(from_mod, const_name)
#       start = ::Qa::Time.now_f
#       $__qa_trace.emit_dur('ActiveSupport::Dependencies#load_missing_constant',
#           'from_mod' => "module #{from_mod}",
#           'const_name' => const_name,
#           'backtrace' => caller) do
#         __qa_original_load_missing_constant(from_mod, const_name)
#       end
#     ensure
#       $__qa_lazily_loaded_constants.push({
#         const_name: const_name,
#         from_module: from_mod,
#         caller: caller_locations,
#         duration: ::Qa::Time.now_f - start,
#       })
#     end
#   end
# end

if defined?(Fabricate)
  X.load_constants_recursively(Fabricate)
end

if defined?(Fabrication)
  X.load_constants_recursively(Fabrication)
end

if defined?(Mail)
  Mail.eager_autoload!
end

$qa_warmup_active_record = lambda do
  if defined?(ActiveRecord::Base)
    # Enumerating columns populates schema caches for the existing connection.
    ActiveRecord::Base.descendants.each do |model|
      # next if model.abstract_class? || !model.table_exists?
      qa_trace.emit_dur('ActiveRecord::Base.descendants columns', 'name' => model.name) do
        begin
          model.columns
        rescue ActiveRecord::StatementInvalid
          nil
        end
      end
    end

    # Eagerly define_attribute_methods on all known models
    ActiveRecord::Base.descendants.each do |model|
      # next if !model.table_exists?
      qa_trace.emit_dur('ActiveRecord::Base.descendants define_attribute_methods', 'name' => model.name) do
        begin
          model.define_attribute_methods
        rescue ActiveRecord::StatementInvalid
          nil
        end
      end
    end
  end

  if defined?(Fabrication)
    Fabrication.manager.schematics.each_value do |schematic|
      schematic.send(:klass)
    end
  end

  if defined?(FactoryGirl)
    # FactoryGirl.factories.each do |factory|
    #   factory.compile
    #   factory.associations
    # end

    FactoryGirl.factories.each do |factory|
      begin
        # Doing this enumerates all necesary classes, etc.
        m = FactoryGirl.build_stubbed(factory.name)
        # Trying to force more eager loading...
        # m.class.reflections.each do |r, v|
        #   if v.is_a?(ActiveRecord::Reflection::ThroughReflection)
        #     $stderr.puts "skipping #{m}.#{r} #{v}"
        #     next
        #   end
        #   $stderr.puts "trying #{m}.#{r} #{v}"
        #   m.send(r)
        # end
      rescue
        $stderr.puts([$!, *$@].join("\n\t"))
      end
    end
  end
end

class TapjConduit
  def initialize(io)
    @io = io
    @mutex = Mutex.new
    @buffer = []
  end

  def emit(doc)
    @mutex.synchronize do
      @buffer.push(doc)
    end
  end

  def flush
    @mutex.synchronize do
      @buffer.each do |doc|
        s = ::Qa::Json.fast_generate(doc, :max_nesting => false)
        s << "\n"
        @io.write s
      end
      @io.flush
      @buffer.clear
    end
  end
end

groups = RSpec.world.ordered_example_groups
groups.each do |group|
  group.descendants.each do |g|
    g.examples.each do |example|
      example.metadata[:object_id] = example.object_id.to_s
    end
  end
end

RSpec.clear_examples

world = ::RSpec.world
config = RSpec.configuration

require 'set'
conserved_classes = Set.new
if defined?(ActiveRecord::ConnectionAdapters::SchemaCache)
  conserved_classes.add(ActiveRecord::ConnectionAdapters::SchemaCache)
end

if false && config.use_transactional_fixtures
  $qa_warmup_active_record.call

  if defined?(ActiveRecord::Base)
    ActiveRecord::Base.connection.disconnect!
  end
else
  # TODO(adamb) Update the below to properly initialize caches for all possible envs
  envs = [
    {'QA_WORKER' => nil},
    {'QA_WORKER' => '1'},
    # {'QA_WORKER' => '2'},
  ]

  connections_by_spec = {}
  envs.each do |env|
    saved = ENV.to_hash.values_at(env.keys)
    env.each do |k, v|
      ENV[k] = v
    end

    spec = Rails.application.config.database_configuration[Rails.env]
    $stderr.puts "Warming up spec #{spec}"
    env.keys.zip(saved).each do |(k, v)|
      ENV[k] = v
    end

    ActiveRecord::Base.establish_connection(spec)
    ActiveRecord::Base.connection_pool.with_connection do
      $qa_warmup_active_record.call
    end
    connection = ActiveRecord::Base.connection_pool.checkout
    connection.disconnect!

    conserved_classes.add(connection.schema_cache.class)
    connections_by_spec[spec] = connection
  end
end


# create reporter with json formatter
reporter = RSpec::Core::Reporter.new(config)
# internal hack
# api may not be stable, make sure lock down Rspec version
loader = config.send(:formatter_loader)
notifications = loader.send(:notifications_for, RSpec::TapjFormatter)

conserved_instances = Hash[conserved_classes.map do |mod|
  [mod, ::ObjectSpace.each_object(mod).to_a]
end]

$__qa_lazily_loaded_constants.clear

# Get a clean GC state, marking remaining objects as old in ruby 2.2
GC.disable
GC.enable
3.times { GC.start }

qa_trace.emit_final_stats

socket.each_line do |line|
  p = Process.fork do
    env, args = JSON.parse(line)

    if tapj_sink_ix = args.index('--tapj-sink')
      args.delete_at(tapj_sink_ix)
      tapj_sink = args.delete_at(tapj_sink_ix)
    end

    if seed_ix = args.index('--seed')
      args.delete_at(seed_ix)
      seed = args.delete_at(seed_ix)
    end
    tapj_conduit = TapjConduit.new(Client.connect(tapj_sink))

    trace_events.each do |e|
      tapj_conduit.emit({'type'=>'trace', 'trace'=>e})
    end

    qa_trace = ::Qa::Trace.new(env['QA_WORKER'] || Process.pid) do |e|
      tapj_conduit.emit({'type'=>'trace', 'trace'=>e})
    end
    trace_probes.each { |trace_probe| qa_trace.define_tracer(trace_probe) }
    qa_trace.start

    env.each do |k, v|
      ENV[k] = v
    end

    formatter = ::RSpec::TapjFormatter.new(qa_trace, tapj_conduit)
    reporter.register_listener(formatter, *notifications)

    if args.delete('--dry-run')
      config.dry_run = true
    else
      if defined?(ActiveRecord::Base)
        qa_trace.emit_dur('ActiveRecord establish_connection') do
          if connections_by_spec
            spec = Rails.application.config.database_configuration[Rails.env]
            connection = connections_by_spec[spec]
            begin
              connection.reconnect!
            rescue PG::ConnectionBad
              # HACK(adamb) For some reason we need to use the private connect method, as there's busted
              #    state inside of the ActiveRecord Postgres connection.
              connection.send(:connect)
            end

            ActiveRecord::Base.connection_pool.checkin(connection)
            # ActiveRecord::Base.establish_connection(spec)
          else
            ActiveRecord::Base.connection.send(:connect)
          end
        end
      end

      world.filter_manager.include(:object_id => lambda { |v| args.include?(v) })
    end

    reporter.report(world.example_count(groups)) do |reporter|
      config.with_suite_hooks do
        groups.each do |g|
          g.run(reporter)
        end
      end
    end

    extra_instances = {}
    conserved_instances.each do |mod, preexisting|
      existing = ::ObjectSpace.each_object(mod).to_a
      extras = existing - preexisting
      unless extras.empty?
        extra_instances[mod] = [extras, preexisting - extras]
      end
    end

    unless extra_instances.empty?
      extra_instances.each do |mod, (extras, gone)|
        extra_ids = extras.map(&:object_id).map { |id| "0x#{id.to_s(16)}" }
        gone_ids = gone.map(&:object_id).map { |id| "0x#{id.to_s(16)}" }
        $stderr.puts "!!! Extra instances found of type #{mod}: #{extra_ids.join(', ')}; gone: #{gone_ids.join(', ')}"
      end
    end

    unless $__qa_lazily_loaded_constants.empty?
      paths = [
        *Rails.application.config.autoload_paths,
      ]

      $__qa_lazily_loaded_constants.each do |entry|
        constant = entry[:const_name]
        duration = entry[:duration]

        if origin = entry[:caller].find { |loc| paths.any? { |path| loc.absolute_path && loc.absolute_path.start_with?(path) } }
          recommendation = "Add the following to #{origin.absolute_path}: require_dependency '...'"
        else
          origin = entry[:from_module]
          recommendation = "Couldn't infer where to put: require_dependency '...'"
        end

        $stderr.puts "!!! Lazily loaded constant in #{duration}s: #{constant} in #{origin}. #{recommendation}"
      end
    end

    tapj_conduit.flush
    exit!
  end
  Process.detach(p)

  trace_events.clear
end
