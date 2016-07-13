RSpec.describe "Flaky" do
  context "flaky context" do
    it "sometimes passes" do
      ENV['QA_FLAKY_TYPE'] == 'assert' ? expect(ENV['QA_FLAKY_1']).to(eq('false')) : (raise "error" if ENV['QA_FLAKY_1'] == 'true')

      expect(ENV['QA_FLAKY_2']).to(eq('false'))
    end
  end
end
