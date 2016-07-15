$__qa_stderr = $stderr.dup

module Qa; end

module Qa::Binding
  begin
    # From https://github.com/banister/binding_of_caller/issues/57
    # Thanks to Steve Shreeve <steve.shreeve@gmail.com>

    require 'fiddle/import'
  rescue LoadError
    class << self
      def callers
        []
      end
    end
  else
    extend Fiddle::Importer

    dlload Fiddle.dlopen(nil)

    DebugStruct = struct [
      "void* thread",
      "void* frame",
      "void* backtrace",
      "void* contexts",
      "long  backtrace_size"
    ]

    extern "void* rb_debug_inspector_open(void*, void*)"
    bind("void* callback(void*, void*)") do |ptr, _|
      DebugStruct.new(ptr).contexts
    end

    class << self
      def callers
        list_ptr = rb_debug_inspector_open(self['callback'], nil)
        list = list_ptr.to_value
        list.drop(4).map {|ary| ary[2] } # grab proper bindings
      end
    end
  end
end

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

class Qa::Stats::LinuxMemory
  def initialize(pid)
    @pid = pid
    @last_sample = nil
    @min_sample_period = 1
  end

  def sample
    now = ::Qa::Time.now_f
    return if @last_sample && ((now - @last_sample) < @min_sample_period)
    @last_sample = now

    h = {}

    status = read_proc_file("status").scan(/(.*):[\t ]+(.*)/)
    unless status.empty?
      h['vm_size'] = status.find { |i| i.first == 'VmSize' }.last.split(' ').first.to_i * 1024.0
      h['vm_rss'] = status.find { |i| i.first == 'VmRSS' }.last.split(' ').first.to_i * 1024.0
    end

    smaps = read_proc_file("smaps").scan(/(.*):[\t ]+(.*)/)
    unless smaps.empty?
      private_dirty = smaps.find_all { |i| i.first == 'Private_Dirty' }
      shared_dirty = smaps.find_all { |i| i.first == 'Shared_Dirty' }

      h['private_dirty_total'] = private_dirty.inject(0) { |i, pd| i + pd.last.split(' ').first.to_i } * 1024.0
      h['shared_dirty_total'] = shared_dirty.inject(0) { |i, sc| i + sc.last.split(' ').first.to_i } * 1024.0
    end

    yield h
  end

  private

  def read_proc_file(name)
    begin
      File.read(File.join("/proc/#{@pid}", name))
    rescue Errno::EACCES, Errno::ENOENT
      ""
    end
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
    ::GC::Profiler.enable
  end

  def sample
    raw_data = ::GC::Profiler.raw_data
    raw_data.each do |gc|
      @last_gc = gc

      # :GC_TIME # Time elapsed in seconds for this GC run
      # :GC_INVOKE_TIME # Time elapsed in seconds from startup to when the GC was invoked
      # :HEAP_USE_SIZE # Total bytes of heap used
      # :HEAP_TOTAL_SIZE # Total size of heap in bytes
      # :HEAP_TOTAL_OBJECTS # Total number of objects
      # :GC_IS_MARKED # Returns true if the GC is in mark phase

      yield(@start_f + gc.delete(:GC_INVOKE_TIME), gc.delete(:GC_TIME) * 1e3, gc)
    end
    ::GC::Profiler.clear unless raw_data.empty?

    nil
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

  @@stackprof = false

  def self.enable_stackprof!
    begin
      # Check for ruby 2.3.0, warn if not using at least stackprof 0.2.9, link to:
      # https://github.com/tmm1/stackprof/commit/21a7c8c67ea6abe26a68e7b117f8b36048e6fbab
      require 'stackprof'
      @@stackprof = true
    rescue LoadError
    end
  end

  def initialize(pid, &b)
    @pid = pid
    @b = b
    @cpu_stats = ::Qa::Stats::Cpu.new
    @gc_stats = ::Qa::Stats::Gc.new
    @linux_memory = ::Qa::Stats::LinuxMemory.new('self')
    @stackprof_result = nil

    @tracers = []
    @uninstall_tracers = []
  end

  require 'tempfile'
  def start
    if @@stackprof
      @stackprof_result_file = Tempfile.new('stackprof')
      @stackprof_result_file.close
      StackProf.start(mode: :wall, raw: true, out: @stackprof_result_file.path)
    end

    @tracers.each do |tracer|
      uninstall = ::Qa::Instrument.tracer(tracer) do |type, val, &b|
        if type == :call
          emit_begin(tracer, ARGS => val ? Hash[val] : nil)
        elsif type == :return
          emit_end(tracer,
              ARGS => {
                'result' => val,
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
    if @@stackprof
      StackProf.stop
      StackProf.results
      @stackprof_result = Marshal.load(IO.binread(@stackprof_result_file.path))
      @stackprof_result_file.close!
    end

    @uninstall_tracers.each(&:call)
    @uninstall_tracers.clear
  end

  # Use constants to avoid needless string creation while emitting stats.
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

    if @stackprof_result
      emit_stackprof_result(@stackprof_result)
    end
  end

  def emit_stats
    emit_cpu
    emit_gc
    emit_threads
    emit_shared_memory

    nil
  end

  private

  def tid
    c = Thread.current
    Thread.main == c ? 1 : c.object_id
  end

  # Use constants to avoid needless string creation while emitting stats.
  FLAMEGRAPH_METRIC_NAME_V2 = 'flamegraph sample v2'.freeze
  RSPEC_PREFIX = 'RSpec::'.freeze
  RSPEC_ELLIPSIS = 'RSpec::...'.freeze
  BLOCK_PREFIX = 'block '.freeze
  STACKTRACE_SEPARATOR = ';'.freeze

  require 'set'
  def emit_stackprof_result(result)
    raw = result[:raw]

    # Build sorted symbol list.
    symbol_set = SortedSet.new
    symbol_set.add(RSPEC_ELLIPSIS)
    result[:frames].each do |_, frame|
      symbol_set.add(frame[:name])
    end

    symbols = symbol_set.to_a
    symbol_set.clear

    # Find index of symbol in sorted list.
    symbol_range = 0...symbols.size
    find_symbol_index = lambda do |symbol|
      symbol_range.bsearch { |i| symbols[i] >= symbol }
    end

    i = 0
    while len = raw[i]
      i += 1
      len.times do
        a = raw[i]
        full_name = result[:frames][a][:name]
        raw[i] = find_symbol_index.call(full_name)
        i += 1
      end
      i += 1
    end

    # Save some space by rewriting symbols to use prefix encoding.
    prev = ''
    symbol_prefixes = Array.new(symbols.length, 0)
    prefix_ix = 0
    symbols.map! do |symbol|
      prefix_len = 0
      symbol_len = symbol.size
      prefix_len += 1 while symbol[prefix_len] == prev[prefix_len] && prefix_len < symbol_len
      prev = symbol
      # Expect to reuse the prefix of the previous symbol. Only keep the end of the string.
      symbol_prefixes[prefix_ix] = prefix_len
      prefix_ix += 1
      symbol[prefix_len..-1]
    end

    emit(
        NAME => FLAMEGRAPH_METRIC_NAME_V2,
        PID => @pid,
        TID => tid,
        PH => PH_I,
        TS => self.ts,
        ARGS => {
          'symbols' => symbols,
          'symbolPrefixes' => symbol_prefixes,
          'samples' => raw,
        })
  end

  # TODO(adamb) Use constants to avoid needless string creation while emitting stats.
  def emit_shared_memory
    @linux_memory.sample do |shared_memory|
      time_f = ::Qa::Time.now_f

      vm_size = shared_memory['vm_size']
      vm_rss = shared_memory['vm_rss']
      emit(
          NAME => 'vm stats',
          PID => @pid,
          PH => PH_C,
          TS => ts(time_f) + 1,
          ARGS => {
            'other' => (vm_size && vm_rss) ? vm_size - vm_rss : nil,
            'rss' => vm_rss,
          })

      emit(
          NAME => 'shared memory stats',
          PID => @pid,
          PH => PH_C,
          TS => ts(time_f) + 1,
          ARGS => {
            'shared_dirty_total' => shared_memory['shared_dirty_total'],
            'private_dirty_total' => shared_memory['private_dirty_total'],
          })
    end
  end

  # Use constants to avoid needless string creation while emitting stats.
  THREAD_METRIC_NAME = 'live threads'.freeze
  THREAD_STATUS_SLEEP = 'sleep'.freeze
  THREAD_STATUS_RUN = 'run'.freeze
  ARGS_RUNNING = 'running'.freeze
  ARGS_SLEEPING = 'sleeping'.freeze
  def emit_threads
    return
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

  # Use constants to avoid needless string creation while emitting stats.
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

  # Use constants to avoid needless string creation while emitting stats.
  GC_METRIC_NAME = 'gc'.freeze
  GC_HEAP_METRIC_NAME = 'heap bytes'.freeze
  ARGS_USED_SIZE_BYTES = 'used_size_bytes'.freeze
  ARGS_FREE_SIZE_BYTES = 'free_size_bytes'.freeze
  GC_METRICS_KEY_USE = :HEAP_USE_SIZE
  GC_METRICS_KEY_TOTAL = :HEAP_TOTAL_SIZE
  def emit_gc
    return unless @gc_stats

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

require 'tempfile'
class Qa::Stdcom
  def self.enable!
    @@enabled = true
  end

  def self.disable!
    @@enabled = false
  end

  enable!

  def initialize
    @out_f = nil
    @err_f = nil
  end

  def reset!
    return unless @@enabled
    @out_f = stdio_tempfile($stdout)
    @err_f = stdio_tempfile($stderr)
  end

  #
  def drain!(doc)
    return doc unless @@enabled && @out_f && @err_f

    stdout = drain_tempfile($stdout, @out_f).chomp("\n")
    stderr = drain_tempfile($stderr, @err_f).chomp("\n")

    doc['stdout'] = stdout unless stdout.empty?
    doc['stderr'] = stderr unless stderr.empty?
  rescue
    $__qa_stderr.puts([$!, *$@].join("\n\t"))
  end

  private

  def stdio_tempfile(io)
    tempfile = Tempfile.new('stdio')

    io.reopen(tempfile.path)
    return tempfile
  end

  def drain_tempfile(wr, tempfile)
    wr.flush
    tempfile.read
  ensure
    tempfile.close!
  end
end

module Qa::TapjExceptions
  module_function

  @@_source_cache = {}

  def summarize_exception(error, backtrace, message=nil)
    # [{"file" => "...", "line" => N, "variables" => {"..." => "", ...}}, ...]

    backtrace_bindings = error.instance_variable_get(:@__qa_caller_bindings)

    backtrace = backtrace.each_with_index.map do |entry, index|
      entry =~ /(.+?):(\d+(?=:|\z))/ || (next nil)

      h = {"file" => $1, "line" => $2.to_i}
      if backtrace_bindings && b = backtrace_bindings[index]
        method, locals = b.eval("[__method__, local_variables]")
        if entry.end_with?("in `#{method}'")
          h['variables'] = Hash[locals.map { |v| [v, b.local_variable_get(v).inspect] }]
        else
          $__qa_stderr.puts "mismatch: '#{entry}' doesn't end with: '#{method}'"
        end
      end

      h
    end

    backtrace = filter_backtrace(backtrace)

    snippets = {} # {"<path>": {N => "...", ...}, ...}
    backtrace.each do |entry|
      file, line = entry["file"], entry["line"]
      snippet = (snippets[file] ||= {})
      snippet.update(code_snippet(file, line))
    end

    {
      'message'   => (message || error.message).strip,
      'class'     => error.class.name,
      'snippets'  => snippets,
      'backtrace' => backtrace,
    }
  end

  # Clean the backtrace of any reference to test framework itself.
  def filter_backtrace(backtrace)
    trace = backtrace

    # eliminate ourselves from the list.
    trace = trace.select { |e| !e['file'].start_with?("-e") }

    ## if the backtrace is empty now then revert to the original
    trace = backtrace if trace.empty?

    ## simplify paths to be relative to current working diectory
    here = Dir.pwd+File::SEPARATOR
    trace.each { |e| e['file'] = e['file'].sub(here,'') }

    trace
  end

  # (number of surrounding lines to show)
  CODE_SNIPPET_RADIUS = 10

  # Returns {N-1 => "...", N => "...", N+1 => "...", ...}
  def code_snippet(file, line)
    return {} unless file && File.file?(file)

    src = source(file)
    region = [line - CODE_SNIPPET_RADIUS, 1].max ..
             [line + CODE_SNIPPET_RADIUS, src.length].min

    Hash[region.map { |n| [n, src[n-1].chomp] }]
  end

  # Cache source file text. This is only used if the TAP-Y stream
  # doesn not provide a snippet and the test file is locatable.
  def source(file)
    return '' if file == '-e' || file == '(eval)'
    @@_source_cache[file] ||= (
      File.readlines(file)
    )
  end
end

class ::Qa::TapjConduit
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

module ::Qa::ClientSocket
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

module ::Qa::Warmup; end

module ::Qa::Warmup::Autoload
  module_function

  def warmup
    if defined?(Rails)
      # Do this earlier, so we can avoid lazy requires and things like
      # ActiveRecord::Base.descendants is fully populated.
      Rails.application.eager_load!

      # Eager load this (it does an internal require)
      Rails.backtrace_cleaner

      # Eager load application routes.
      Rails.application.routes.routes
    end

    if defined?(Rack)
      load_constants_recursively(Rack)
    end

    if defined?(I18n)
      I18n.fallbacks[I18n.locale] if I18n.respond_to? :fallbacks
      I18n.default_locale
    end

    if defined?(Fabricate)
      load_constants_recursively(Fabricate)
    end

    if defined?(Fabrication)
      load_constants_recursively(Fabrication)
    end

    if defined?(Mail)
      Mail.eager_autoload!
    end

    if defined?(ActionController)
      require 'action_controller/metal/testing'
    end
  end

  def load_constants_recursively(mod)
    visited = Set.new
    soon = [mod]

    while m = soon.pop
      visited.add(m)
      m.constants(false).each do |c|
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

require 'set'
class ::Qa::ConservedInstancesSet
  def initialize
    @classes = Set.new
  end

  def add_class(c)
    @classes.add(c)
  end

  def remember!
    @instances = Hash[@classes.map do |mod|
      [mod, ::ObjectSpace.each_object(mod).to_a]
    end]
  end

  def check_conservation
    extra_instances = {}
    @instances.each do |mod, preexisting|
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
  end
end

module ::Qa::Warmup::RailsActiveRecord
  module_function

  def rails_database_configuration
    Rails.application.config.database_configuration[Rails.env]
  end

  def resume(cache, env)
    if defined?(Rails) && defined?(ActiveRecord::Base) &&
        connection_cache = cache[:rails_database_connections]
      connection = connection_cache[env]

      begin
        connection.reconnect!
      rescue PG::ConnectionBad
        # HACK(adamb) For some reason we need to use the private connect method, as there's busted
        #    state inside of the ActiveRecord Postgres connection.
        connection.send(:connect)
      end

      ActiveRecord::Base.connection_pool.checkin(connection)
    end
  end

  def with_env(env)
    saved = ENV.to_hash.values_at(env.keys)
    env.each do |k, v|
      ENV[k] = v
    end

    yield
  ensure
    env.keys.zip(saved).each do |(k, v)|
      ENV[k] = v
    end
  end

  def warmup_envs(envs)
    cache = Hash.new { |h, k| h[k] = {} }

    if defined?(Rails) && defined?(ActiveRecord::Base)
      default_rails_db_cfg = rails_database_configuration
      default_db = default_rails_db_cfg['database']
    end

    envs.each do |env|
      with_env(env) do
        if defined?(Rails) && defined?(ActiveRecord::Base)
          connection_cache = cache[:rails_database_connections]
          config = rails_database_configuration
          if config['database'] == default_db && envs.length > 1 && defined?(ActiveRecord::NoDatabaseError)
            config['database'] = "#{config['database']}_qa#{env['QA_WORKER']}"

            $__qa_stderr.puts "Warming up (overridden) config #{config}"
            ActiveRecord::Base.establish_connection(config)
            begin
              ActiveRecord::Base.connection
            rescue ActiveRecord::NoDatabaseError
              # HACK(adamb) Need to create it now!
              $__qa_stderr.puts "WARNING the given database DOES NOT EXIST YET! Will create it now."
              ActiveRecord::Base.configurations       = {}
              ActiveRecord::Migrator.migrations_paths = ActiveRecord::Tasks::DatabaseTasks.migrations_paths
              ActiveRecord::Tasks::DatabaseTasks.current_config = config
              ActiveRecord::Tasks::DatabaseTasks.create(config)
              ActiveRecord::Base.establish_connection(config)
              ActiveRecord::Base.connection
              ActiveRecord::Tasks::DatabaseTasks.migrate
            end
          else
            $stderr.puts "Warming up config #{config}"
            ActiveRecord::Base.establish_connection(config)
          end

          ActiveRecord::Base.connection_pool.with_connection do
            warmup_connection
          end
          connection = ActiveRecord::Base.connection_pool.checkout
          connection.disconnect!

          connection_cache[env] = connection
        end
      end
    end

    cache.default_proc = nil
    cache.freeze
    cache
  end

  def warmup_connection
    (ActiveRecord::Base.connection.tables - %w[schema_migrations]).each do |table|
      table.classify.constantize.first rescue nil
    end

    if defined?(SeedFu)
      SiteSetting.automatically_download_gravatars = false
      SeedFu.seed
    end

    # # warm up AR
    # RailsMultisite::ConnectionManagement.each_connection do
    #   (ActiveRecord::Base.connection.tables - %w[schema_migrations]).each do |table|
    #     table.classify.constantize.first rescue nil
    #   end
    # end

    # if defined?(ActiveRecord::Base)
    #   # Enumerating columns populates schema caches for the existing connection.
    #   ActiveRecord::Base.descendants.each do |model|
    #     begin
    #       model.columns
    #     rescue ActiveRecord::StatementInvalid
    #       nil
    #     end
    #   end
    #
    #   # Eagerly define_attribute_methods on all known models
    #   ActiveRecord::Base.descendants.each do |model|
    #     begin
    #       model.define_attribute_methods
    #     rescue ActiveRecord::StatementInvalid
    #       nil
    #     end
    #   end
    # end

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
end

class ::Qa::ClientOptionParser
  attr_reader :dry_run
  attr_reader :trace_probes
  attr_reader :seed
  attr_reader :tapj_sink

  def initialize
    @trace_probes = []
    @dry_run = false
    @seed = nil

    @opt = OptionParser.new do |opts|
      opts.on "--help", "Display this help." do
        puts opts
        exit
      end

      desc = "Sets random seed. Also via env. Eg: SEED=n rake"
      opts.on "--seed SEED", Integer, desc do |m|
        @seed = m.to_i
      end

      opts.on "--trace-probe PROBE" do |p|
        @trace_probes.push(p)
      end

      opts.on "--dry-run" do
        @dry_run = true
      end

      opts.on "--tapj-sink ENDPOINT" do |s|
        @tapj_sink = s
      end
    end
  end

  def parse(args)
    @opt.parse(args)
  end
end

class ::Qa::TestEngine
  def initialize
    @prefork = lambda {}
    @run_tests = lambda {}
  end

  def def_prefork(&b)
    @prefork = b
  end

  def def_run_tests(&b)
    @run_tests = b
  end

  def main(args)
    trace_probes = []
    while trace_probe_ix = args.index('--trace-probe')
      args.delete_at(trace_probe_ix)
      trace_probes.push(args.delete_at(trace_probe_ix))
    end

    # Collect trace events for transmission later.
    trace_events = []
    qa_trace = ::Qa::Trace.new(Process.pid) { |e| trace_events.push(e) }
    trace_probes.each { |trace_probe| qa_trace.define_tracer(trace_probe) }

    socket = ::Qa::ClientSocket.connect(args.shift)

    # Read first line from socket to get initial config.
    initial_json = socket.gets
    initial_config = JSON.parse(initial_json)
    worker_envs, initial_files = initial_config['workerEnvs'], initial_config['files']
    passthrough = initial_config['passthrough']

    if passthrough['sampleStack']
      Qa::Trace.enable_stackprof!
    end
    qa_trace.start

    # Delegate prefork actions.
    @prefork.call(initial_files)

    conserved = ::Qa::ConservedInstancesSet.new

    # Autoload constants.
    if passthrough['warmup']
      ::Qa::Warmup::Autoload.warmup

      # Once we've warmed up, we don't want more SchemaCache instances created. If we see more,
      # then there's something wrong with our warmup (or resume). Add the class to our conserved
      # set.
      if defined?(ActiveRecord::ConnectionAdapters::SchemaCache)
        conserved.add_class(ActiveRecord::ConnectionAdapters::SchemaCache)
      end

      # Warm up each worker environment.
      cache = ::Qa::Warmup::RailsActiveRecord.warmup_envs(worker_envs)
    end

    # From here on, we don't expect to see new instances for any of our conserved classes.
    conserved.remember!

    # Get a clean GC state, marking remaining objects as old in ruby 2.2
    3.times { GC.start }

    # The hard work is done for now. Emit statistics so they'll be ready for
    # transmission later.
    qa_trace.emit_final_stats

    # Decide whether or not we're capturing stdout, stderr during test runs.
    if passthrough['captureStandardFds']
      Qa::Stdcom.enable!
    else
      Qa::Stdcom.disable!
    end

    case passthrough['errorsCaptureLocals'].to_s
    when 'true'
      TracePoint.new(:raise) do |tp|
        e = tp.raised_exception
        c = ::Qa::Binding.callers
        e.instance_variable_set(:@__qa_caller_bindings, c)
      end.enable
    when 'TracePoint.new(:raise)'
      TracePoint.new(:raise) do |tp|
        e = tp.raised_exception
        c = ::Qa::Binding.callers
        e.instance_variable_set(:@__qa_caller_bindings, c)
      end.enable
    when 'Exception#initialize'
      ::Exception.class_exec do
        alias :__qa_original_initialize :initialize
        def initialize(*args, &b)
          __qa_original_initialize(*args, &b)

          @__qa_caller_bindings = ::Qa::Binding.callers
        end
      end
    end

    eval_after_fork = (passthrough['evalAfterFork'] || '')

    socket.each_line do |line|
      env, args = JSON.parse(line)

      resume = passthrough['warmup']
      accept_client(cache, env, args, eval_after_fork, resume, conserved, trace_probes, trace_events)

      # Only pass trace_events along to the first client.
      trace_events.clear
    end
  end

  def accept_client(cache, env, args, eval_after_fork, resume, conserved, trace_probes, trace_events)
    p = Process.fork do
      opt = ::Qa::ClientOptionParser.new
      tests = opt.parse(args)

      seed = opt.seed
      tapj_conduit = ::Qa::TapjConduit.new(::Qa::ClientSocket.connect(opt.tapj_sink))

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

      unless opt.dry_run
        if resume
          ::Qa::Warmup::RailsActiveRecord.resume(cache, env)
        end

        eval(eval_after_fork) unless eval_after_fork.empty?
      end

      @run_tests.call(qa_trace, opt, tapj_conduit, tests)

      conserved.check_conservation

      tapj_conduit.flush
      exit!
    end
    Process.detach(p)
  end
end
