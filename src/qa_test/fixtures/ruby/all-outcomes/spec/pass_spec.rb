$zero = 0
RSpec.describe "Pass" do
  context "world of passing" do
    it "always passes" do
      expect(0).to eq $zero
      # To ensure multiple runs don't overlap!
      $zero = 'zero'
    end
  end
end
