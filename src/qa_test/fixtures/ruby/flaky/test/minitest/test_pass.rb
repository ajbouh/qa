require 'minitest/unit'

class MinitestPassTest < Minitest::Test
  def test_pass
    assert_equal(0, 0)
  end
end
