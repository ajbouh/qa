# This file should actually be ignored.

RSpec.describe "Ignored" do
  context "void" do
    it "never gets here" do
      expect(0).to eq 1
    end
  end
end
