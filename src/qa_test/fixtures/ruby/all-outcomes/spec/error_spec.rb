# Try to pollute LOAD_PATH.
require 'pathname'

$LOAD_PATH.push(Pathname.new("foo"))

toplevel = 1
RSpec.describe "Error" do
  describelevel = "two"
  context "world of errors" do
    contextlevel = :three
    it "always errors" do
      itlevel = [{"four"=>4}]
      raise "Always an error"
    end
  end
end
