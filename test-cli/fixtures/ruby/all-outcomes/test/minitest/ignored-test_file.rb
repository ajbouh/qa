require 'minitest/unit'

# This file should actually be ignored.
class MinitestTestIgnored < Minitest::Test
  def test_ignored
    assert(false)
  end
end
