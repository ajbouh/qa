package emitter

import (
	"errors"
	"qa/runner"
	"qa/runner/ruby"
	"qa/runner/server"
	"qa/tapjio"
)

type Emitter interface {
	EnumerateTests() ([]tapjio.TraceEvent, []runner.TestRunner, error)
	Close() error
}

type emitterStarter func(
	srv *server.Server,
	workerEnvs []map[string]string,
	runnerConfig runner.Config) (Emitter, error)

func rubyEmitterStarter(runnerAssetName string) emitterStarter {
	return func(
		srv *server.Server,
		workerEnvs []map[string]string,
		runnerConfig runner.Config) (Emitter, error) {

		config := &ruby.ContextConfig{
			RunnerConfig:    runnerConfig,
			Rubylib:         []string{"spec", "lib", "test"},
			RunnerAssetName: runnerAssetName,
		}

		ctx, err := ruby.StartContext(config, srv, workerEnvs)
		if err != nil {
			return nil, err
		}

		return ctx, nil
	}
}

var starters = map[string]emitterStarter{
	"rspec":     rubyEmitterStarter("ruby/rspec.rb"),
	"minitest":  rubyEmitterStarter("ruby/minitest.rb"),
	"test-unit": rubyEmitterStarter("ruby/test-unit.rb"),
}

var defaultGlobs = map[string]string{
	"rspec":     "spec/**/*spec.rb",
	"minitest":  "test/**/test*.rb",
	"test-unit": "test/**/test*.rb",
}

func DefaultGlob(name string) string {
	return defaultGlobs[name]
}

func Resolve(
	srv *server.Server,
	workerEnvs []map[string]string,
	config runner.Config) (Emitter, error) {
	starter, ok := starters[config.Name]
	if !ok {
		return nil, errors.New("Could not find starter: " + config.Name)
	}

	return starter(srv, workerEnvs, config)
}
