RSpec.describe "Fail" do
  context "world of fails" do
    it "always fails" do
      expect(0).to eq 1
    end
  end
end
