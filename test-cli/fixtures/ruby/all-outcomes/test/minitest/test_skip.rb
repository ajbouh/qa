require 'minitest/unit'

class MinitestSkipTest < Minitest::Test
  def test_skip
    sleep 1
    skip
  end
end
