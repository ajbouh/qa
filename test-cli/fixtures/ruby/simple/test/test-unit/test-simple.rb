require 'test/unit'
require 'my-library'

class SimpleTestUnitTest < Test::Unit::TestCase
  def test_library
    MyLibrary.new
  end

  def test_sleep
    sleep 2
  end
end
