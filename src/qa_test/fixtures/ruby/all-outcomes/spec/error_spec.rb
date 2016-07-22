# Try to pollute LOAD_PATH.
require 'pathname'

$LOAD_PATH.push(Pathname.new("foo"))

RSpec.describe "Error" do
  context "world of errors" do
    it "always errors" do
      raise "Always an error"
    end
  end
end
