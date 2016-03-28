require 'test/unit'

class TestUnitSkipTest < Test::Unit::TestCase
  def test_skip
    sleep 1
    omit
  end
end
