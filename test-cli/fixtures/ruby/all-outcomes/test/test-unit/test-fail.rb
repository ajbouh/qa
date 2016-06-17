require 'test/unit'

class TestUnitTestFail < Test::Unit::TestCase
  def test_fail
    sleep 1
    val = 1
    assert_equal(0, val)
  end
end
