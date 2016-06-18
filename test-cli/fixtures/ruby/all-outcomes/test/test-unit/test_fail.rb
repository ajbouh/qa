require 'test/unit'

class MyTestCaseClass < Test::Unit::TestCase
end

class TestUnitTestFail < MyTestCaseClass
  def test_fail
    sleep 1
    val = 1
    assert_equal(0, val)
  end

  def test_duplicate_method_name
    sleep 1
    assert_equal(0, 1)
  end
end
