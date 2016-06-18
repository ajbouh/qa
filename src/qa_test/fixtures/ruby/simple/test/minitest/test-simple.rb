require 'minitest/unit'
require 'my-library'

class SimpleMinitestTest < Minitest::Test
  def test_library_minitest
    MyLibrary.new
  end

  def test_sleep
    sleep 2
  end
end
