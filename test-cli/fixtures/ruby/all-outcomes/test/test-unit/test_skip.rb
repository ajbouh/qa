require 'test/unit'

class TestUnitSkipTest < Test::Unit::TestCase
  def test_skip
    sleep 1
    omit
  end

  def test_duplicate_method_name
    omit
  end
end
