RSpec.describe "Error" do
  context "world of errors" do
    it "always errors" do
      raise "Always an error"
    end
  end
end
