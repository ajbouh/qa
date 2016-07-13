RSpec.describe "Fail" do
  context "world of fails" do
    it "always fails" do
      val = 1
      expect(0).to eq val
    end
  end
end
