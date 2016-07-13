require 'minitest/unit'

class MinitestTestError < Minitest::Test
  def test_error
    sleep 1
    raise "Always an error"
  end
end
