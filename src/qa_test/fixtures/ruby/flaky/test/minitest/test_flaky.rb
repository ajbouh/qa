require 'minitest/unit'

class MinitestFlakyTest < Minitest::Test
  def test_flaky
    ENV['QA_FLAKY_TYPE'] == 'assert' ? assert_equal('false', ENV['QA_FLAKY_1']) : (raise "error" if ENV['QA_FLAKY_1'] == 'true')

    assert_equal('false', ENV['QA_FLAKY_2'])
  end
end