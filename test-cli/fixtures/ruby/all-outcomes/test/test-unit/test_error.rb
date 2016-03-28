require 'test/unit'

class TestUnitTestError < Test::Unit::TestCase
  def test_error
    sleep 1
    raise "Always an error"
  end
end
