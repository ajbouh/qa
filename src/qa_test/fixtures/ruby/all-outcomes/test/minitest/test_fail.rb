require 'minitest/unit'

class MinitestTestFail < Minitest::Test
  def test_fail
    naplength = 1
    sleep naplength
    assert_equal(0, 1)
  end
end
