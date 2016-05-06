require 'base64'

module Qa; end

module Qa::Instrument
  module_function

  def tracer(expr, &trace_block)
    if expr =~ /^(.+?)\((.+)\)$/
      name, arg_name_expr = $1, $2
      arg_names = arg_name_expr.split(',').map(&:strip)
    else
      name = expr
      arg_names = nil
    end
    name =~ /^(.+?)([\.#])(.+)$/
    mod_name, method_type, method_sym = $1, $2, $3.to_sym
    mod = eval("defined?(#{mod_name}) ? #{mod_name} : nil")

    install = lambda do |method|
      define_method(method_sym) do |*args, &b|
        trace_block.call(:call, arg_names ? arg_names.zip(args) : nil)
        begin
          result = method.bind(self).call(*args, &b)
        ensure
          trace_block.call(:return, result)
        end
      end
    end

    return nil unless mod

    case method_type
    when '#'
      method = mod.instance_method(method_sym)
      mod.module_exec(method, &install)
      uninstall = lambda do
        mod.module_exec do
          define_method(method_sym, method)
        end
      end
    when '.'
      method = mod.method(method_sym)
      mod.instance_exec(method, &install)
      uninstall = lambda do
        method.owner.instance_exec do
          # undefine_singleton_method(method_sym)
          define_method(method_sym, method)
        end
      end
    end

    uninstall
  end
end

class Qa::Strobe
  def self.start(*args, &b)
    new(*args, &b).start
  end

  def initialize(sleep_time, &b)
    @sleep_time = sleep_time
    @b = b

    @mutex = Mutex.new
    @cvar = ConditionVariable.new
    @wants_to_stop = false
  end

  def start
    raise "Already started" if @thr

    @thr = Thread.start do
      @mutex.synchronize do
        until @wants_to_stop do
          before = Qa::Time.now_f
          @b.call
          after = Qa::Time.now_f

          @cvar.wait(@mutex, @sleep_time - (after - before))
        end
      end
    end

    self
  end

  def stop
    return unless @thr

    @mutex.synchronize do
      @wants_to_stop = true
      @cvar.broadcast
    end

    @thr.join
    @thr = nil
  end
end

module Qa::Json
  require 'json'
  PrivateJson = ::JSON

  module_function
  def fast_generate(*args)
    PrivateJson.fast_generate(*args)
  end
end

module Qa::Time
  PrivateTime = ::Time.dup

  module_function
  if defined? Process::CLOCK_MONOTONIC
    def strftime(time_f, format)
      PrivateTime.at(time_f).strftime(format)
    end

    def now_f
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end
  else
    def strftime(time_f, format)
      PrivateTime.at(time_f).strftime(format)
    end

    def now_f
      PrivateTime.now.to_f
    end
  end
end

module Qa::Backtrace
  module_function

  def cleanup(backtrace)
    load_path = $LOAD_PATH
    backtrace.reverse_each.map do |line|
      if line =~ /(.*)\:(\d+)\:in `(.*)'/
        file, number, method = $1, $2, $3
      else
        file, number, method = line, 0, nil
      end
      if prefix = load_path.find { |p| file.start_with?(p) }
        file = file[(prefix.length+1)..-1]
      end
      block_given? ? yield(file, number, method) : "#{file}:#{number}:in `#{method}'"
    end
  end
end

module Qa::Stats; end

class Qa::Stats::Flamegraph
  def initialize(thr)
    @queue = Queue.new
    @strobe = ::Qa::Strobe.start(1.0/97) do
      b = thr.backtrace
      if b
        status = thr.status
        if status != 'run'
          b.unshift(":0:in `<#{status}>'")
        end
        @queue.push(b)
      end
    end
  end

  def sample
    counts = Hash.new(0)

    @queue.size.times do
      backtrace = @queue.pop

      last = nil
      key = ::Qa::Backtrace.cleanup(backtrace) do |file, lineno, method|
        if file.start_with?('rspec/')
          next nil if last == 'rspec/...'
          next last = 'rspec/...'
        end
        if file.end_with?('_spec.rb')
          next nil if last == '.../*_spec.rb'
          next last = '.../*_spec.rb'
        end

        last = "#{file}:#{method}:#{lineno}"
      end
      key.compact!

      counts[key.join(';')] += 1
    end

    yield counts unless counts.empty?
  end
end

class Qa::Stats::Cpu
  def initialize
    @cpu_start = ::Qa::Time.now_f
    @cpu_times = Process.times
  end

  def sample
    prv_start, prv_times = @cpu_start, @cpu_times
    @cpu_start, @cpu_times = ::Qa::Time.now_f, Process.times

    duration = @cpu_start - prv_start

    utime = @cpu_times.utime - prv_times.utime
    stime = @cpu_times.stime - prv_times.stime

    duration = [duration, utime + stime].max
    if duration == 0
      usr, sys = 0, 0
    else
      usr = (100.0 * utime / duration).round(2)
      sys = (100.0 * stime / duration).round(2)
    end

    idl = (100.0 - (usr + sys)).round(2)

    yield(prv_start, sys, usr, idl)
  end
end

class Qa::Stats::GcStats
  def initialize
  end

  def sample
    # From https://samsaffron.com/archive/2013/11/22/demystifying-the-ruby-gc
    # count: the number of times a GC ran (both full GC and lazy sweep are included)

    # heap_used: the number of heaps that have more than 0 slots used in
    #     them. The larger this number, the slower your GC will be.

    # heap_length: the total number of heaps allocated in memory.
    #     For example 1648 means - about 25.75MB is allocated to Ruby heaps.
    #     (1648 * (2 << 13)).to_f / (2 << 19)

    # heap_increment: Is the number of extra heaps to be allocated, next time
    #     Ruby grows the number of heaps (as it does after it runs a GC and
    #     discovers it does not have enough free space), this number is updated
    #     each GC run to be 1.8 * heap_used. In later versions of Ruby this
    #     multiplier is configurable.

    # heap_live_num: This is the running number objects in Ruby heaps, it will
    #     change every time you call GC.stat

    # heap_free_num: This is a slightly confusing number, it changes after a GC
    #     runs, it will let you know how many objects were left in the heaps
    #     after the GC finished running. So, in this example we had 102447 slots
    #     empty after the last GC. (it also increased when objects are recycled
    #     internally - which can happen between GCs)

    # heap_final_num: Is the count of objects that were not finalized during the
    #     last GC.
  end
end

class Qa::Stats::Gc
  def initialize
    @start_f = ::Qa::Time.now_f
    GC::Profiler.enable
  end

  def sample
    # HACK(adamb) Would prefer to use GC::Profiler.raw_data in ruby 2.1.0
    gc_result_text = GC::Profiler.result

    # event = base_event('GarbageCollection')
    if gc_result_text
      GC::Profiler.clear
      parse_gc_result_text(gc_result_text).each do |gc|
        @last_gc = gc
        yield(gc.delete('invoke_time'), gc.delete('gc_duration_ms'), gc)
      end
    end

    nil
  end

  private

  def parse_gc_result_text(gc_text_result)
    gc_text_data = gc_text_result.lines.drop(2)
    gc_text_data.map do |line|
      index, invoke_time_s, use_size_bytes, total_size_bytes, total_object, gc_duration_ms = line.split(' ')

      {
        # 'index' => index.to_i,
        'invoke_time' => @start_f + invoke_time_s.to_f,
        'use_size_bytes' => use_size_bytes.to_i,
        'total_size_bytes' => total_size_bytes.to_i,
        'total_object' => total_object.to_i,
        'gc_duration_ms' => gc_duration_ms.to_f
      }
    end
  end
end

# Outputs Trace Event Format designed to work with the chrome://tracing viewer.
# The event format is documented here:
#   https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU/edit
# More information about the viewer is available here:
#   https://code.google.com/p/trace-viewer/
class Qa::Trace
  require 'thread'
  require 'json'

  @@ruby_prof = false
  # begin
  #   require 'ruby-prof'
  #   @@ruby_prof = true
  # rescue LoadError
  # end

  @@perftools = false
  # begin
  #   require 'perftools'
  #   require 'zlib'
  #   require 'base64'
  #   require 'tempfile'
  #   @@perftools = true
  # rescue LoadError
  # end

  @@stackprof = false
  begin
    # Check for ruby 2.3.0, warn if not using at least stackprof 0.2.9, link to:
    # https://github.com/tmm1/stackprof/commit/21a7c8c67ea6abe26a68e7b117f8b36048e6fbab
    require 'stackprof'
    @@stackprof = true
  rescue LoadError
  end

  @@flamgraph_polling = !(@@ruby_prof || @@perftools || @@stackprof)
  @@flamgraph_polling = false

  def initialize(pid, &b)
    @pid = pid
    @b = b
    @cpu_stats = ::Qa::Stats::Cpu.new
    @gc_stats = ::Qa::Stats::Gc.new

    @perftool_pprof_file = nil
    @flamegraph_stats = nil
    @ruby_prof_result = nil
    @stackprof_result = nil

    if @@flamgraph_polling
      @flamegraph_stats = ::Qa::Stats::Flamegraph.new(Thread.current)
    end

    @tracers = []
    @uninstall_tracers = []

    # define_tracer('Kernel#require(path)')
    # define_tracer('Kernel#load')
    define_tracer('ActiveRecord::ConnectionAdapters::Mysql2Adapter#execute(sql,name)')
    define_tracer('ActiveSupport::Dependencies::Loadable#require(path)')
    define_tracer('ActiveRecord::ConnectionAdapters::QueryCache#clear_query_cache')
    define_tracer('ActiveRecord::ConnectionAdapters::SchemaCache#initialize')
    define_tracer('ActiveRecord::ConnectionAdapters::SchemaCache#clear!')
    define_tracer('ActiveRecord::ConnectionAdapters::SchemaCache#clear_table_cache!')
  end

  def start
    @strobe = ::Qa::Strobe.new(0.25) do
      emit_stats
    end

    RubyProf.start if @@ruby_prof

    if @@perftools
      @perftool_pprof_file = Tempfile.new('pprof')
      @perftool_pprof_file.close
      PerfTools::CpuProfiler.start(@perftool_pprof_file.path)
    end

    if @@stackprof
      StackProf.start(mode: :wall, raw: true)
    end

    @tracers.each do |tracer|
      uninstall = ::Qa::Instrument.tracer(tracer) do |type, val, &b|
        if type == :call
          emit_begin(tracer, ARGS => val ? Hash[val] : nil)
        elsif type == :return
          emit_end(tracer,
              ARGS => {
                'result' => val,
                # 'caller' => caller[1],
                'caller' => caller(1),
              })
        end
      end
      @uninstall_tracers.push(uninstall) if uninstall
    end
  end

  def define_tracer(tracer)
    @tracers.push(tracer)
  end

  def stop
    @strobe.stop
    @ruby_prof_result = RubyProf.stop if @@ruby_prof
    if @@stackprof
      StackProf.stop
      @stackprof_result = StackProf.results
    end

    PerfTools::CpuProfiler.stop if @@perftools

    @uninstall_tracers.each(&:call)
    @uninstall_tracers.clear
  end

  NAME = 'name'.freeze
  PID = 'pid'.freeze
  PH = 'ph'.freeze
  TID = 'tid'.freeze
  TS = 'ts'.freeze
  ARGS = 'args'.freeze
  ARGS_DATA = 'data'.freeze
  DUR = 'dur'.freeze
  PH_E = 'E'.freeze
  PH_B = 'B'.freeze
  PH_X = 'X'.freeze
  PH_I = 'I'.freeze
  PH_C = 'C'.freeze

  def emit_dur(name, args=nil)
    event = {
      NAME => name,
      PID => @pid,
      PH => PH_B,
      TID => tid,
      TS => self.ts,
      ARGS => args
    }
    emit(event)

    h = {}
    yield h
  ensure
    # NOTE(adamb) Use tid during return so we don't get confused by forks.
    emit(event.merge(PH => PH_E, TS => self.ts, ARGS => h, TID => tid))
  end

  def emit_begin(name, h=nil)
    emit(
        {
          NAME => name,
          PID => @pid,
          TID => tid,
          PH => PH_B,
          TS => (h && h[TS]) ? nil : self.ts,
        }, h)

    nil
  end

  def emit_end(name, h=nil)
    emit(
        {
          NAME => name,
          PID => @pid,
          TID => tid,
          PH => PH_E,
          TS => (h && h[TS]) ? nil : self.ts,
        }, h)

    nil
  end

  def emit(e, h=nil)
    @b.call(h ? e.merge(h) : e)
  end

  def ts(time=nil)
    (time || ::Qa::Time.now_f) * 1e6
  end

  def emit_final_stats
    emit_stats
    stop

    if @last_heap_bytes_event
      emit(@last_heap_bytes_event.merge(TS => self.ts))
    end

    if @ruby_prof_result
      emit_ruby_prof_result(@ruby_prof_result)
    end

    if @stackprof_result
      emit_stackprof_result(@stackprof_result)
    end

    if @perftool_pprof_file
      pprof_path = @perftool_pprof_file.path
      symbols_path = "#{pprof_path}.symbols"

      emit_pprof_file(pprof_path, symbols_path)
      @perftool_pprof_file.unlink
      File.unlink(symbols_path)
      @perftool_pprof_file = nil
    end
  end

  def emit_stats
    emit_cpu
    emit_gc
    emit_threads
    emit_flamegraph

    nil
  end

  private

  def tid
    c = Thread.current
    Thread.main == c ? 1 : c.object_id
  end

  def gz_base64(path)
    sio = StringIO.new
    gzw = Zlib::GzipWriter.new(sio)
    gzw.mtime = 0
    gzw << File.read(path)
    gzw.close
    Base64.strict_encode64(sio.string)
  end

  PPROF_METRIC_NAME = 'pprof data'.freeze
  def emit_pprof_file(pprof_path, symbols_path)
    now = self.ts

    args = {
      'pprofGzBase64' => gz_base64(pprof_path),
      'symbolsGzBase64' => gz_base64(symbols_path),
    }

    dur = (::Qa::Time.now_f * 1e6) - now

    emit(
        NAME => PPROF_METRIC_NAME,
        PID => @pid,
        TID => tid,
        PH => PH_X,
        TS => now,
        DUR => dur,
        ARGS => args)
  end

  FLAMEGRAPH_METRIC_NAME = 'flamegraph sample'.freeze
  RSPEC_PREFIX = 'RSpec::'.freeze
  RSPEC_ELLIPSIS = 'RSpec::...'.freeze
  BLOCK_PREFIX = 'block '.freeze
  STACKTRACE_SEPARATOR = ';'.freeze

  def emit_stackprof_result(result)
    raw = result[:raw]

    counts = []
    i = 0
    while len = raw[i]
      i += 1
      key = ''
      len.times do
        a = raw[i]
        i += 1

        full_name = result[:frames][a][:name]
        next if full_name.start_with?(BLOCK_PREFIX)

        if full_name.start_with?(RSPEC_PREFIX)
          next if key.end_with?(RSPEC_ELLIPSIS)
          full_name = RSPEC_ELLIPSIS
        end

        key << STACKTRACE_SEPARATOR unless key.empty?
        key << full_name
      end
      weight = raw[i]
      i += 1

      counts.push([key, weight])
    end

    emit(
        NAME => FLAMEGRAPH_METRIC_NAME,
        PID => @pid,
        TID => tid,
        PH => PH_I,
        TS => self.ts,
        ARGS => {
          ARGS_DATA => counts,
        })
  end

  def emit_ruby_prof_result(result)
    # overall_threads_time = result.threads.reduce(0) { |a, thread| a + thread.total_time }
    result.threads.each do |thread|
      current_thread_id = thread.fiber_id
      overall_time = thread.total_time

      print_stack = proc do |counts, prefix, call_info|
        total_time = call_info.total_time
        percent_total = (total_time/overall_time)*100
        next unless percent_total > 0
        next unless total_time >= 0.01

        kids = call_info.children
        method = call_info.target
        full_name = method.full_name.to_s
        if full_name.start_with?('RSpec::') || full_name.start_with?('(Class::RSpec::')
          full_name = 'RSpec::...'
          if prefix.end_with?('RSpec::...')
            current = prefix
          else
            current = prefix.empty? ? full_name : "#{prefix};#{full_name}"
          end
        else
          current = prefix.empty? ? full_name : "#{prefix};#{full_name}"
        end
        counts.push([current,call_info.self_time * 1e3])

        kids.each do |child|
          print_stack.call(counts, current, child)
        end
      end

      start = ""
      # start << "Thread:#{thread.id}"
      # start << "Fiber:#{thread.fiber_id}" unless thread.id == thread.fiber_id
      thread.methods.each do |m|
        next unless m.root?
        m.call_infos.each do |ci|
          next unless ci.root?
          counts = []
          print_stack.call(counts, start, ci)

          emit(
              NAME => FLAMEGRAPH_METRIC_NAME,
              PID => @pid,
              TID => 1,
              PH => PH_I,
              TS => self.ts,
              ARGS => {
                ARGS_DATA => counts,
              })
        end
      end
    end
  end

  def emit_flamegraph
    return unless @flamegraph_stats
    @flamegraph_stats.sample do |counts|
      emit(
          NAME => FLAMEGRAPH_METRIC_NAME,
          PID => @pid,
          TID => 1,
          PH => PH_I,
          TS => self.ts,
          ARGS => {
            ARGS_DATA => counts,
          })
    end
  end

  THREAD_METRIC_NAME = 'live threads'.freeze
  THREAD_STATUS_SLEEP = 'sleep'.freeze
  THREAD_STATUS_RUN = 'run'.freeze
  ARGS_RUNNING = 'running'.freeze
  ARGS_SLEEPING = 'sleeping'.freeze
  def emit_threads
    running = 0
    sleeping = 0
    statuses = Thread.list.each do |thr|
      case thr.status
      when THREAD_STATUS_SLEEP
        sleeping += 1
      when THREAD_STATUS_RUN
        running += 1
      end
    end

    emit(
        NAME => THREAD_METRIC_NAME,
        PID => @pid,
        PH => PH_C,
        TS => self.ts,
        ARGS => {
          ARGS_RUNNING => running,
          ARGS_SLEEPING => sleeping,
        })
  end

  CPU_METRIC_NAME = 'cpu usage'.freeze
  CPU_SYSTEM = 'system'.freeze
  CPU_USER = 'user'.freeze
  CPU_IDLE = 'idle'.freeze
  def emit_cpu
    @cpu_stats.sample do |time_f, sys, usr, idl|
      emit(
          NAME => CPU_METRIC_NAME,
          PID => @pid,
          PH => PH_C,
          TS => ts(time_f) + 1,
          ARGS => {
            CPU_SYSTEM => sys,
            CPU_USER => usr,
            CPU_IDLE => idl,
          })
    end
  end

  GC_METRIC_NAME = 'gc'.freeze
  GC_HEAP_METRIC_NAME = 'heap bytes'.freeze
  ARGS_USED_SIZE_BYTES = 'used_size_bytes'.freeze
  ARGS_FREE_SIZE_BYTES = 'free_size_bytes'.freeze
  GC_METRICS_KEY_USE = 'use_size_bytes'.freeze
  GC_METRICS_KEY_TOTAL = 'total_size_bytes'.freeze
  def emit_gc
    prev = @last_heap_bytes_event
    @gc_stats.sample do |gc_time_f, gc_dur_ms, gc_metrics|
      invoke_time_ts = self.ts(gc_time_f)

      dur = gc_dur_ms * 1000.0
      emit(
          NAME => GC_METRIC_NAME,
          PID => @pid,
          TID => 0,
          PH => PH_X,
          TS => invoke_time_ts,
          DUR => dur,
          ARGS => gc_metrics)

      used = gc_metrics[GC_METRICS_KEY_USE]
      @last_heap_bytes_event = {
        NAME => GC_HEAP_METRIC_NAME,
        PID => @pid,
        PH => PH_C,
        TS => invoke_time_ts + dur,
        ARGS => {
          ARGS_USED_SIZE_BYTES => used,
          ARGS_FREE_SIZE_BYTES => gc_metrics[GC_METRICS_KEY_TOTAL] - used,
        }
      }

      emit(@last_heap_bytes_event)
    end
  end
end

module Qa::TapjExceptions
  module_function

  @@_source_cache = {}

  def summarize_exception(error, backtrace, message=nil)
    e_file, e_line = location(backtrace)
    r_file = e_file.sub(Dir.pwd+'/', '')

    {
      'message'   => clean_message(message || error.message),
      'class'     => error.class.name,
      'file'      => r_file,
      'line'      => e_line,
      'source'    => (source(e_file)[e_line-1] || '').strip,
      'snippet'   => code_snippet(e_file, e_line),
      'backtrace' => filter_backtrace(backtrace)
    }
  end

  def clean_message(message)
    message.strip #.gsub(/\s*\n\s*/, "\n")
  end

  # Clean the backtrace of any reference to test framework itself.
  def filter_backtrace(backtrace)
    ## remove backtraces that match any pattern in IGNORE_CALLERS
    # trace = backtrace.reject{|b| $RUBY_IGNORE_CALLERS.any?{|i| i=~b}}

    ## remove `:in ...` portion of backtraces
    trace = backtrace.map do |bt|
      i = bt.index(':in')
      i ? bt[0...i] :  bt
    end

    ## now apply MiniTest's own filter (note: doesn't work if done first, why?)
    trace = Minitest::filter_backtrace(trace) if defined?(Minitest)

    ## if the backtrace is empty now then revert to the original
    trace = backtrace if trace.empty?

    ## simplify paths to be relative to current workding diectory
    trace = trace.map{ |bt| bt.sub(Dir.pwd+File::SEPARATOR,'') }

    return trace
  end

  # Returns a String of source code.
  def code_snippet(file, line)
    s = []
    if File.file?(file)
      source = source(file)
      radius = 2 # TODO: make customizable (number of surrounding lines to show)
      region = [line - radius, 1].max ..
               [line + radius, source.length].min

      s = region.map do |n|
        {n => source[n-1].chomp}
      end
    end
    return s
  end

  # Cache source file text. This is only used if the TAP-Y stream
  # doesn not provide a snippet and the test file is locatable.
  def source(file)
    return '' if file == '-e' || file == '(eval)'
    @@_source_cache[file] ||= (
      File.readlines(file)
    )
  end

  # Parse source location from caller, caller[0] or an Exception object.
  def parse_source_location(caller)
    case caller
    when Exception
      trace  = caller.backtrace.reject{ |bt| bt =~ INTERNALS }
      caller = trace.first
    when Array
      caller = caller.first
    end
    caller =~ /(.+?):(\d+(?=:|\z))/ or return ""
    source_file, source_line = $1, $2.to_i
    return source_file, source_line
  end

  # Get location of exception.
  def location backtrace # :nodoc:
    last_before_assertion = ""
    backtrace.reverse_each do |s|
      break if s =~ /in .(assert|refute|flunk|pass|fail|raise|must|wont)/
      last_before_assertion = s
    end
    file, line = last_before_assertion.sub(/:in .*$/, '').split(':')
    line = line.to_i if line
    return file, line
  end
end
