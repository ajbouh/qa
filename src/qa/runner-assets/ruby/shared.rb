$__qa_stderr = $stderr.dup

$__qa_load_event_queue = nil

require 'digest/sha2'

module Kernel
  alias_method :__qa_original_require, :require
  def require(name)
    file_stack = (Thread.current[:__qa_current_file] ||= [])
    prev = file_stack[-1]

    file_stack.push([:require, name])
    result = __qa_original_require(name)

    if $__qa_load_event_queue
      entry = {
        operation: :require,
        name: name,
        prev: prev
      }
      # $__qa_stderr.puts "enqueuing load event: #{entry}"
      $__qa_load_event_queue.enq(entry)
    end

    result
  rescue LoadError
    unless $!.instance_variable_get(:@__qa_load_path)
      $!.instance_variable_set(:@__qa_load_path, $LOAD_PATH.dup)
    end

    unless $!.instance_variable_get(:@__qa_path)
      $!.instance_variable_set(:@__qa_path, $!.path)
    end

    raise
  ensure
    file_stack.pop if file_stack
  end

  # Since the native implementation of require_relative peeks at the
  # stack, we need to simulate its behavior in ruby, by ourselves
  # peeking at the stack.
  alias_method :__qa_original_require_relative, :require_relative
  QA_FEATURE_EXTENSIONS = [".rb", "", ".so", ".o", ".dll"]
  def require_relative(name)
    file_stack = (Thread.current[:__qa_current_file] ||= [])
    prev = file_stack[-1]

    calling_absolute_path = Kernel.caller_locations(1, 1)[0].absolute_path
    caller_dir = File.dirname(calling_absolute_path)
    loaded_feature_base = File.expand_path(name, caller_dir)
    loaded_feature = loaded_feature_base
    QA_FEATURE_EXTENSIONS.each do |ext|
      path = loaded_feature_base + ext
      if File.file?(path)
        loaded_feature = path
        break
      end
    end

    file_stack.push([:require_relative, loaded_feature])

    result = __qa_original_require(loaded_feature)

    if $__qa_load_event_queue
      entry = {
        operation: :require_relative,
        loaded_feature: loaded_feature,
        prev: prev,
      }
      # $__qa_stderr.puts "enqueuing load event: #{entry}"
      $__qa_load_event_queue.enq(entry)
    end

    result
  rescue LoadError
    unless $!.instance_variable_get(:@__qa_path)
      $!.instance_variable_set(:@__qa_path, $!.path)
    end

    raise
  ensure
    file_stack.pop if file_stack
  end

  alias_method :__qa_original_load, :load
  def load(path, wrap=false)
    file_stack = (Thread.current[:__qa_current_file] ||= [])
    prev = file_stack[-1]

    loaded_feature = File.expand_path(path)

    file_stack.push([:load, loaded_feature])
    result = __qa_original_load(path, wrap)

    if $__qa_load_event_queue
      entry = {
        operation: :load,
        path: path,
        loaded_feature: loaded_feature,
        prev: prev,
      }
      # $__qa_stderr.puts "enqueuing load event: #{entry}"
      $__qa_load_event_queue.enq(entry)
    end

    result
  rescue LoadError
    unless $!.instance_variable_get(:@__qa_path)
      $!.instance_variable_set(:@__qa_path, loaded_feature)
    end

    raise
  ensure
    file_stack.pop if file_stack
  end
end


module Qa; end

require 'thread'
require 'set'
class ::Qa::LoadTracking
  class FileDigestPool
    def initialize(digests)
      @recently_added_files = []
      @digests = digests
      @index_by_file = Hash.new { |h, k| @recently_added_files.push(k); h[k] = h.size }
    end

    def intern(files)
      file_indices = []
      files.each do |file|
        file_indices.push(@index_by_file[file])
      end

      if @recently_added_files.empty?
        return nil, nil, file_indices
      end

      recently_added_files, @recently_added_files = @recently_added_files, []
      recently_added_digests = Array.new(recently_added_files.size)
      recently_added_files.each_with_index do |file, ix|
        recently_added_digests[ix] = @digests[file]
      end

      return recently_added_files, recently_added_digests, file_indices
    end
  end

  FEATURE_EXTENSIONS = [
    ".rb",
    "",
    ".so",
    ".o",
    ".dll",
  ]

  def initialize
    @digests = {}
    @loaded_feature_cache = []
    @loaded_files_by_file = Hash.new { |h, k| h[k] = Set.new }
    @previous_loaded_features_length = 0
  end

  def new_file_digest_pool
    ::Qa::LoadTracking::FileDigestPool.new(@digests)
  end

  def files_loaded_by(feature_path)
    @loaded_files_by_file[feature_path]
  end

  def during(prev_default)
    trap_begin
    yield
  ensure
    trap_end(prev_default)
  end

  def trap_begin
    @orig_queue, $__qa_load_event_queue = $__qa_load_event_queue, Queue.new

    nil
  end

  def trap_end(default_prev)
    $__qa_load_event_queue, queue = @orig_queue, $__qa_load_event_queue
    @orig_queue = nil

    events = Array.new(queue.length)
    queue.length.times { |n| events[n] = queue.pop }

    index_recently_loaded_feature_paths!

    events.each do |e|
      unless loaded_feature = (e[:loaded_feature] || lookup_loaded_feature(e[:name]))
        next
      end

      next unless prev = e[:prev] || default_prev
      calling_absolute_path = case prev[0]
      when :require
        lookup_loaded_feature(prev[1])
      when :require_relative
        prev[1]
      when :load
        prev[1]
      when :all
        :all
      end

      recursive_add_feature(calling_absolute_path, loaded_feature)
    end

    nil
  end

  private

  def lookup_loaded_feature(required_feature)
    if value = @loaded_feature_cache.bsearch { |elt| required_feature <=> elt[0] }
      return value[1]
    end

    return required_feature if File.exist?(required_feature)
    return nil
  end

  def index_recently_loaded_feature_paths!
    if @previous_loaded_features_length == 0
      loaded_features = $LOADED_FEATURES.sort
    else
      loaded_features = $LOADED_FEATURES[@previous_loaded_features_length..-1]
      loaded_features.sort!
    end

    unless loaded_features.empty?
      @previous_loaded_features_length = $LOADED_FEATURES.length
      load_path = $LOAD_PATH.map { |s| s.is_a?(String) ? s : s.to_s }
      load_path.sort!

      load_path_ix = 0
      load_path_entry = load_path[load_path_ix]
      load_path_entry_length = load_path_entry.length
      sep = File::SEPARATOR
      for loaded_feature in loaded_features
        ext_len = File.extname(loaded_feature).length
        loaded_feature_no_ext = loaded_feature[0...(-ext_len)]

        if loaded_feature.start_with?(load_path_entry) && loaded_feature[load_path_entry_length] == sep
          loaded_feature_subpath = loaded_feature[(load_path_entry_length+1)..-1]
          @loaded_feature_cache.push([loaded_feature_subpath, loaded_feature])
          loaded_feature_subpath_no_ext = loaded_feature_subpath[0...(-ext_len)]
          @loaded_feature_cache.push([loaded_feature_subpath_no_ext, loaded_feature])
        elsif loaded_feature > load_path_entry
          load_path_ix += 1
          if load_path_entry = load_path[load_path_ix]
            load_path_entry_length = load_path_entry.length
            redo
          else
            break
          end
        end

        @loaded_feature_cache.push([loaded_feature_no_ext, loaded_feature])
      end

      @loaded_feature_cache.sort!
    end
  end

  def recursive_add_feature(caller_path, feature_path)
    loaded_feature_set = @loaded_files_by_file[caller_path]
    return unless loaded_feature_set.add?(feature_path)

    digest_for_feature_path(feature_path)
    @loaded_files_by_file[feature_path].each do |path|
      recursive_add_feature(caller_path, path)
    end
  end

  def digest_for_feature_path(feature_path)
    if digest = @digests[feature_path]
      digest
    end

    digest = File.file?(feature_path) ? Digest::SHA2.file(feature_path).hexdigest : ""
    @digests[feature_path] = digest

    digest
  end
end

module ::Qa::Binding
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
      time = PrivateTime.now + (time_f - now_f)
      time.strftime(format)
    end

    def now_f
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    def at_f(time_f)
      PrivateTime.now.to_f + (time_f - now_f)
    end
  else
    def strftime(time_f, format)
      PrivateTime.at(time_f).strftime(format)
    end

    def at_f(time_f)
      time_f
    end

    def now_f
      PrivateTime.now.to_f
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
    # [
    #   {
    #     "file" => "...",
    #     "method" => "...",
    #     "line" => N,
    #     "variables" => {"..." => "", ...}
    #   },
    #   ...
    # ]

    backtrace_bindings = error.instance_variable_get(:@__qa_caller_bindings)
    load_path = $LOAD_PATH.map(&:to_s)

    unless message
      if error.is_a?(SyntaxError)
        if error.message =~ /\A([^:]+:\d+):\s+(.*)$/
          message = $2
          backtrace = [$1, *backtrace]
          backtrace_bindings = [nil, *backtrace_bindings]
        end
      elsif error.is_a?(LoadError)
        backtrace = backtrace[1..-1]
        backtrace_bindings = backtrace_bindings[1..-1] if backtrace_bindings
      end

      message ||= error.message
    end

    # eliminate ourselves from the list.
    internal_file_patterns = [
      /-e/,
      %r%minitest.rb|minitest/%
    ]
    here = File.realpath(Dir.pwd) + File::SEPARATOR

    backtrace = backtrace.each_with_index.map do |entry, index|
      entry =~ /(.+?):(\d+)(?:\:in `(.*)')?/ || (next nil)

      raw_file = $1
      line = $2.to_i
      method = $3
      block_level = 0

      # e.g.: block (2 levels) in run
      if method =~ /^block \((\d+) levels?\) in (.*)$/
        block_level = $1.to_i
        method = $2
      # e.g.: block in run
      elsif method =~ /^block in (.*)$/
        block_level = 1
        method = $1
      end

      # Simplify paths as much as we can.
      if raw_file == "-e" || raw_file == "(eval)"
        file = raw_file
      elsif raw_file.start_with?(here)
        file = raw_file.sub(here, './')
      elsif prefix = load_path.find { |p| raw_file.start_with?(p) }
        file = raw_file[(prefix.length+1)..-1]
      else
        file = raw_file
      end

      h = {"raw-file" => raw_file, "line" => line, "file" => file}
      h["method"] = method if method
      h["block_level"] = block_level if block_level && block_level > 0
      h["internal"] = internal_file_patterns.any? { |re| re =~ file }
      if !h["internal"] && backtrace_bindings && b = backtrace_bindings[index]
        locals = b.eval("local_variables")
        h['variables'] = Hash[locals.map { |v| [v, b.local_variable_get(v).inspect] }]
      end

      h
    end

    backtrace = filter_backtrace(backtrace)

    snippets = {} # {"<path>": {N => "...", ...}, ...}
    backtrace.each do |entry|
      raw_file, file, line = entry.delete("raw-file"), entry["file"], entry["line"]
      snippet = (snippets[file] ||= {})
      snippet.update(code_snippet(raw_file, line))
    end

    h = {
      'message'   => message.strip,
      'class'     => error.class.name,
      'snippets'  => snippets,
      'backtrace' => backtrace,
    }

    if error.is_a?(LoadError)
      h['load_error_path'] = error.instance_variable_get(:@__qa_path) || error.path
      if load_path = error.instance_variable_get(:@__qa_load_path)
        h['load_error_load_path'] = load_path
      end
    end

    h
  end

  # Clean the backtrace of any reference to test framework itself.
  def filter_backtrace(bt)
    new_bt = bt.take_while { |e| !e['internal'] }
    new_bt = bt.select     { |e| !e['internal'] } if new_bt.empty?
    new_bt = bt.dup                               if new_bt.empty?

    new_bt
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

class ::Qa::JsonConduit
  def initialize(io)
    @io = io
    @mutex = Mutex.new
    @buffer = []
  end

  def emit(event)
    @mutex.synchronize do
      @buffer.push(event)
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

class ::Qa::TapjConduit
  # TAP-Y/J Revision
  REVISION = 4

  def initialize(load_tracking, upstream)
    @upstream = upstream
    @load_tracking = load_tracking
    @loaded_feature_digest_pool = load_tracking.new_file_digest_pool

    @mutex = Mutex.new

    # Maps "absolute path" to [list absolute paths that might have been used, if present]
    @missing_file_dependencies = {}

    # List of test events we should emit after the suite event.
    @preloaded_test_events = []

    @total_count = 0
    @fail_count = 0
    @error_count = 0
    @pass_count = 0
    @omit_count = 0
    @todo_count = 0
  end

  def emit_test_begin_event(start_time, label, subtype, filter, file=nil)
    emit(
        'type' => 'note',
        'qa:type' => 'test:begin',
        'qa:timestamp' => ::Qa::Time.at_f(start_time),
        'qa:label' => label,
        'qa:subtype' => subtype,
        'qa:filter' => filter,
        'qa:file' => file)
    flush
  end

  def preloaded_test_events=(events)
    @mutex.synchronize do
      @preloaded_test_events = events.dup
    end
  end

  def missing_file_dependencies=(deps)
    @mutex.synchronize do
      @missing_file_dependencies = deps.dup
    end
  end

  def emit_suite_event(now, count, seed)
    preloaded_test_events = @mutex.synchronize do
      p = @preloaded_test_events.dup
      @preloaded_test_events.clear
      p
    end

    emit(
        'type'  => 'suite',
        'start' => ::Qa::Time.strftime(now, '%Y-%m-%d %H:%M:%S'),
        'count' => count + preloaded_test_events.length,
        'seed'  => seed,
        'rev'   => REVISION)

    preloaded_test_events.each do |event|
      emit_test_begin_event(
          now,
          event['label'],
          event['subtype'],
          event['filter'])

      emit(event)
    end

    flush
  end

  # This method is invoked after the dumping of examples and failures.
  def emit_final_event(duration)
    event = @mutex.synchronize do
      {
        'type' => 'final',
        'time' => duration,
        'counts' => {
          'total' => @total_count,
          'pass'  => @pass_count,
          'fail'  => @fail_count,
          'error' => @error_count,
          'omit'  => @omit_count,
          'todo'  => @todo_count
        }
      }
    end

    emit(event)
    flush
  end

  def passed?
    @mutex.synchronize do
      (@fail_count + @error_count).zero?
    end
  end

  def emit(event)
    @mutex.synchronize do
      case event['type']
      when 'note'
        if event['qa:type'] == 'test:begin'
          @load_tracking.trap_begin
        end
      when 'test'
        if file = event['file']
          absolute_path = File.expand_path(file)
          missing_files = @missing_file_dependencies[absolute_path] || []

          qa_feature_extensions = [".rb", ".so", ".o", ".dll"]
          include_absolute_path = ->(absolute_path) do
            if qa_feature_extensions.any? { |ext| absolute_path.end_with?(ext) }
              missing_files.push(absolute_path)
            else
              qa_feature_extensions.each { |ext| missing_files.push(absolute_path + ext) }
            end
          end

          if exception = event['exception']
            if error_path = exception['load_error_path']
              if error_path == File.expand_path(error_path) # is absolute (may be load or absolute require)
                include_absolute_path.(error_path)
              elsif load_path = exception['load_error_load_path'] # must be non-absolute require
                load_path.each { |dir| include_absolute_path.(File.expand_path(error_path, dir)) }
              end
            end
          end

          @load_tracking.trap_end([:load, absolute_path])

          loaded_features = @load_tracking.files_loaded_by(absolute_path)
          new_files, new_digests, file_indices = @loaded_feature_digest_pool.intern(loaded_features)
          if new_files
            @upstream.emit({'type' => 'note', 'qa:type' => 'dependency', 'files' => new_files, 'digests' => new_digests})
          end

          # Augment test events with a list of dependencies
          event['dependencies'] = {
            'loaded_indices' => file_indices,
            'missing' => missing_files
          }
        end

        @total_count += 1

        case event['status']
        when 'fail'
          @fail_count += 1
        when 'error'
          @error_count += 1
        when 'pass'
          @pass_count += 1
        when 'omit'
          @omit_count += 1
        when 'todo'
          @todo_count += 1
        end
      end
    end

    @upstream.emit(event)
  end

  def flush
    @upstream.flush
  end
end

module ::Qa::ClientSocket
  module_function

  require 'socket'
  def connect(address)
    if /^(.*)@([^:]+):(\d+)$/ =~ address
      token, ip, port = $1, $2, $3
      socket = TCPSocket.new(ip, port)
      socket.write("#{token}\n")
      socket.flush
      socket
    elsif /^(.*)@([^:]+)$/ =~ address
      token, unix = $1, $2, $3
      socket = UNIXSocket.new(unix)
      socket.write("#{token}\n")
      socket.flush
      socket
    else
      abort("Malformed address: #{address}")
    end
  end
end

module ::Qa::EagerLoad; end

module ::Qa::EagerLoad::Autoload
  module_function

  def eager_load!
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

module ::Qa::Warmup; end
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
            config['database'] = "#{config['database']}_qa#{env['TEST_ENV_NUMBER']}"

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

require 'set'
class ::Qa::TestEngine
  def initialize
    @prefork = lambda {}
    @run_tests = lambda {}
    @load_error_by_file = {}
    @script_errors_by_file = Hash.new { |h, k| h[k] = [] }

    @load_tracking = ::Qa::LoadTracking.new
  end

  def def_prefork(&b)
    @prefork = b
  end

  def def_run_tests(&b)
    @run_tests = b
  end

  def load_file(file)
    absolute_path = File.expand_path(file)
    @load_tracking.during(nil) { load(absolute_path) }
  rescue ScriptError, Exception
    if $!.is_a?(LoadError)
      @load_error_by_file[absolute_path] = $!
    end
    @script_errors_by_file[absolute_path].push($!)
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
    $LOAD_PATH.concat(initial_config['rubylib'] || [])
    worker_envs, initial_files = initial_config['workerEnvs'], initial_config['files']
    passthrough = initial_config['passthrough']

    if passthrough['sampleStack']
      Qa::Trace.enable_stackprof!
    end
    qa_trace.start

    eval_before_fork = (passthrough['evalBeforeFork'] || '')
    unless eval_before_fork.empty?
      @load_tracking.during([:all]) do
        eval(eval_before_fork)
      end
    end

    # Copy globally loaded files for everyone.
    # globally_loaded_files = @loaded_files_by_file.delete(:all) || {}
    # $__qa_stderr.puts "globally_loaded_files: #{globally_loaded_files}"
    # @loaded_files_by_file.each do |file, digests|
    #   digests.merge!(globally_loaded_files)
    # end

    # Delegate prefork actions.
    (initial_files || []).each do |file|
      load_file(file)
    end

    [
      @script_errors_by_file,
      @load_error_by_file,
    ].each { |h| h.default_proc = nil }

    qa_feature_extensions = [".rb", ".so", ".o", ".dll"]
    @missing_file_dependencies = {}
    @load_error_by_file.each do |file, error|
      missing_files = []
      include_absolute_path = ->(absolute_path) do
        if qa_feature_extensions.any? { |ext| absolute_path.end_with?(ext) }
          missing_files.push(absolute_path)
        else
          qa_feature_extensions.each { |ext| missing_files.push(absolute_path + ext) }
        end
      end

      error_path = error.instance_variable_get(:@__qa_path) || error.path
      if error_path == File.expand_path(error_path) # is absolute (may be load or absolute require)
        include_absolute_path.(error_path)
      elsif load_path = error.instance_variable_get(:@__qa_load_path) # must be non-absolute require
        load_path.each { |dir| include_absolute_path.(File.expand_path(error_path, dir)) }
      end

      @missing_file_dependencies[file] = missing_files
    end

    @prefork.call

    conserved = ::Qa::ConservedInstancesSet.new

    # Autoload constants.
    if passthrough['eagerLoad']
      ::Qa::EagerLoad::Autoload.eager_load!
    end

    if passthrough['warmup']
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
      begin
        opt = ::Qa::ClientOptionParser.new
        tests = opt.parse(args)

        seed = opt.seed
        socket = ::Qa::ClientSocket.connect(opt.tapj_sink)
        tapj_conduit = ::Qa::TapjConduit.new(@load_tracking, ::Qa::JsonConduit.new(socket))
        tapj_conduit.missing_file_dependencies = @missing_file_dependencies

        run_everything = tests.empty?

        script_errors_with_file = []
        @script_errors_by_file.each do |file, script_errors|
          script_errors.each do |script_error|
            script_errors_with_file.push([file, script_error])
          end
        end

        # If we're doing a dry run, then we're going to emit the script errors no matter what.
        # Otherwise only emit errors that match the given tests.
        unless opt.dry_run || tests.empty?
          script_errors_with_file.select! { |(file, script_error)| tests.delete("#{file}:0") }
        end

        unless script_errors_with_file.empty?
          preloaded_test_events = []
          script_errors_with_file.each do |(file, script_error)|
            exception = ::Qa::TapjExceptions.summarize_exception(
                script_error,
                script_error.backtrace)

            preloaded_test_events.push({
              'type'      => 'test',
              'subtype'   => 'script',
              'status'    => 'error',
              'filter'    => "#{file}:0",
              'label'     => file,
              'file'      => file,
              'line'      => 0,
              'time'      => 0,
              'exception' => exception
            })
          end
          tapj_conduit.preloaded_test_events = preloaded_test_events
        end

        trace_events.each do |e|
          tapj_conduit.emit({'type'=>'trace', 'trace'=>e})
        end

        qa_trace = ::Qa::Trace.new(env['TEST_ENV_NUMBER'] || Process.pid) do |e|
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

        if tests.empty? && !run_everything
          tapj_conduit.emit_suite_event(::Qa::Time.now_f, 0, seed)
          tapj_conduit.emit_final_event(0)
        else
          @run_tests.call(qa_trace, opt, tapj_conduit, tests)
        end

        conserved.check_conservation
      ensure
        tapj_conduit.flush if tapj_conduit
        socket.close if socket
      end
      exit!
    end
    Process.detach(p)
  end
end
