require 'minitest/unit'

$zero = 0
class MinitestPassTest < Minitest::Test
  def test_pass
    assert_equal 0, $zero
    $zero = 'zero'
  end
end
