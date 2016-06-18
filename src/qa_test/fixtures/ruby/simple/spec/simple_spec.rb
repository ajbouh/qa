require 'my-library'

RSpec.describe MyLibrary, ".new" do
  it "my library rspec" do
    MyLibrary.new
  end

  it "sleeps" do
    sleep 2
  end
end
