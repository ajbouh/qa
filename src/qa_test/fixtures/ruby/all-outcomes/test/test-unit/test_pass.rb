require 'test/unit'

$zero = 0
class TestUnitPassTest < Test::Unit::TestCase
  def test_pass
    assert_equal 0, $zero
    $zero = 'zero'
  end

  def test_duplicate_method_name
  end
end
