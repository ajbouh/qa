require 'minitest/unit'

class MinitestTestFail < Minitest::Test
  def test_fail
    sleep 1
    assert_equal(0, 1)
  end
end
