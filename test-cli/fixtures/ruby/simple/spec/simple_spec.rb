require 'my-library'

RSpec.describe MyLibrary, ".new" do
  it "works" do
    MyLibrary.new
  end

  it "sleeps" do
    sleep 2
  end
end
