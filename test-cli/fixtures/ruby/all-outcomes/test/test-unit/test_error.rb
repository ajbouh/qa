require 'test/unit'

class TestUnitTestError < Test::Unit::TestCase
  def test_error
    sleep 1
    foo = 1
    longVariableNameToo = "some string value"
    raise "Always an error"
  end
end
