module TapjExceptions
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
      'source'    => source(e_file)[e_line-1].strip,
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
