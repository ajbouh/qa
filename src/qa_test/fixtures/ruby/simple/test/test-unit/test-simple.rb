require 'test/unit'
require 'my-library'

class SimpleTestUnitTest < Test::Unit::TestCase
  def test_library_test_unit
    MyLibrary.new
  end

  def test_sleep
    sleep 2
  end
end
