#!/usr/bin/env ruby

require 'set'
require 'json'

class TapjResultFilter
  def initialize
    # Ids that are immediately printed
    @primary_whitelist = Set.new
    @secondary_whitelist = Set.new

    # Map from id to buffered line
    @buffers = Hash.new { |h, k| h[k] = [] }

    @should_keep_buffer = ->(t, rest) { true }
    @primary_id = nil
    @secondary_id = nil
  end

  def primary_id(&b)
    @primary_id = b
  end

  def secondary_id(&b)
    @secondary_id = b
  end

  def keep_buffer_if(&b)
    @should_keep_buffer = b
  end

  # Returns [accepted, discarded]
  def process(enumerator, &b)
    raise "Missing @primery_id" unless @primary_id

    accepted = 0
    accepted_ids = 0

    enumerator.each do |t|
      id_count, record_count = accept(t) do |accepted_t|
        if @secondary_id
          @secondary_whitelist.add(@secondary_id.call(accepted_t))
        end
        b.call(accepted_t)
      end
      accepted_ids += id_count
      accepted += record_count
    end

    finally_accepted_ids, finally_accepted, discarded_ids, discarded = finish(&b)

    accepted += finally_accepted
    accepted_ids += finally_accepted_ids

    [accepted_ids, accepted, discarded_ids, discarded]
  end

  private

  # Processes an element t, yielding it to the block given if either:
  # - @primary_id.call(t) is in @primary_whitelist
  # - @should_keep_buffer.call(@primary_id.call(t), t, buffer), where
  #   buffer is a list of entries with a matching primary id that previously
  #   failed these conditions.
  # If @should_keep_buffer.call is true, also yield the entries of buffer.
  def accept(t, &output)
    accepted = 0
    accepted_ids = 0

    id = @primary_id.call(t)

    if @primary_whitelist.member?(id)
      accepted += 1
      output.call(t)
    else
      buffer = @buffers[id]
      buffer.push(t)

      if @should_keep_buffer.call(id, t, buffer)
        @primary_whitelist.add(id)
        accepted_ids += 1
        @buffers.delete(id)
        buffer.each { |t| output.call(t) }
        accepted += buffer.length
      end
    end

    [accepted_ids, accepted]
  end

  # Processes elements not previously yielded to the block provided
  # to accept. Individual elements are yielded to the block given if either:
  # - @secondary_id.call(t) is in @secondary_whitelist
  # - @should_keep_buffer.call(@primary_id.call(t), nil, buffer), where
  #   buffer is a list of entries a matching primary id that previously
  #   failed these conditions.
  def finish(&output)
    accepted = 0
    accepted_ids = 0
    discarded = 0
    discared_ids = 0

    use_secondary = !@secondary_whitelist.empty?

    @buffers.keys.each do |id|
      residue = @buffers[id]

      if use_secondary
        residue.reject! do |t|
          id2 = @secondary_id.call(t)
          next false unless @secondary_whitelist.member?(id2)

          accepted_ids +=1
          output.call(t)

          true
        end
      end

      if @should_keep_buffer.call(id, nil, residue)
        @primary_whitelist.add(id)
        accepted_ids += 1

        residue.each do |t|
          accepted += 1
          output.call(t)
        end
      else
        discared_ids += 1
        discarded += residue.length
      end
    end
    @buffers.clear

    [accepted_ids, accepted, discared_ids, discarded]
  end
end

keep_tolerates_nil = true
predicates = []
filter = TapjResultFilter.new
me = File.basename($0)

require 'optparse'
opts = OptionParser.new do |opts|
  opts.banner = <<-EOH
Usage: #{me} [OPTIONS]

Iterates over STDIN lines, parsing each as standalone JSON. Collapses lines
into collections based on the specified collapse id. Flushes all collapsed lines
according to rules specified as arguments.

Example(s):
  #{me} --collapse-id suite.label,case.label,label --keep-if-any status==error --keep-if-any status==fail
  EOH

  opts.on('--collapse-id OTHER.SUBKEY,KEY2,...',
      "Specify keys to form id to collapse on") do |keys|
    # Split by , and respect . values
    keys = keys.split(',').map { |key| key.split('.') }

    filter.primary_id do |t|
      # Get value based on sequence of key lookups.
      keys.map { |key| key.inject(t) { |a, k| a[k] } }
    end
  end

  opts.on('--[no-]tolerate-nil',
      "Specify whether nil in a collapse id will prevent keep") do |b|
    keep_tolerates_nil = b
  end

  opts.on('--keep-if-any KEY==JSONVALUE',
      "Flush all collapsed results if specified key equals given value") do |pred|
    key, value = pred.split('==', 2)
    key = key.split('.')
    value = JSON.parse("[#{value}]").first rescue value
    predicates.push([key, value])
  end

  opts.on('--keep-residual-records-matching-kept OTHER.SUBKEY,KEY2,...') do |keys|
    # Split by , and respect . values
    keys = keys.split(',').map { |key| key.split('.') }

    filter.secondary_id do |t|
      # Get a list of values from
      keys.map { |key| key.inject(t) { |a, k| a && a[k] } }
    end
  end
end

opts.parse(ARGV)

filter.keep_buffer_if do |id, t, residue|
  if t
    next false if !keep_tolerates_nil && id.any?(&:nil?)

    predicates.any? do |(key, value)|
      key.inject(t) { |a, k| a[k] } == value
    end
  end
end

accepted_ids, accepted, discarded_ids, discarded = filter.process(
    $stdin.each_line.map { |line| JSON.parse(line) }) do |t|
  $stdout.puts(JSON.generate(t))
end

$stderr.printf("discarded %4d/%4d ids, %4d/%4d records",
    discarded_ids, discarded_ids + accepted_ids,
    discarded, accepted + discarded)
$stderr.puts(": #{me} #{ARGV.join(' ')}")
