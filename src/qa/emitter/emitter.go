package emitter

import (
	"errors"
	"qa/runner"
	"qa/runner/ruby"
	"qa/runner/server"
	"qa/tapjio"
)

type Emitter interface {
	TraceProbes() []string
	EnumerateTests() ([]tapjio.TraceEvent, []runner.TestRunner, error)
}

type emitterStarter func(
	srv *server.Server,
	passthroughConfig map[string](interface{}),
	workerEnvs []map[string]string,
	dir string,
	env map[string]string,
	seed int,
	files []string) (Emitter, error)

// Enable entries below to add specific method calls (and optionally their arguments) to the trace.
var rubyTraceProbes = []string{
// "Kernel#require(path)",
// "Kernel#load",
// "ActiveRecord::ConnectionAdapters::Mysql2Adapter#execute(sql,name)",
// "ActiveRecord::ConnectionAdapters::PostgresSQLAdapter#execute_and_clear(sql,name,binds)",
// "ActiveSupport::Dependencies::Loadable#require(path)",
// "ActiveRecord::ConnectionAdapters::QueryCache#clear_query_cache",
// "ActiveRecord::ConnectionAdapters::SchemaCache#initialize",
// "ActiveRecord::ConnectionAdapters::SchemaCache#clear!",
// "ActiveRecord::ConnectionAdapters::SchemaCache#clear_table_cache!",
}

func rubyEmitterStarter(runnerAssetName string, policy ruby.SquashPolicy) emitterStarter {
	return func(
		srv *server.Server,
		passthroughConfig map[string](interface{}),
		workerEnvs []map[string]string,
		dir string,
		env map[string]string,
		seed int,
		files []string) (Emitter, error) {

		config := &ruby.ContextConfig{
			Dir:               dir,
			EnvVars:           env,
			Seed:              seed,
			Rubylib:           []string{"spec", "lib", "test"},
			RunnerAssetName:   runnerAssetName,
			TraceProbes:       rubyTraceProbes,
			SquashPolicy:      policy,
			PassthroughConfig: passthroughConfig,
		}

		ctx, err := ruby.StartContext(config, srv, workerEnvs, files)
		if err != nil {
			return nil, err
		}

		return ctx, nil
	}
}

var starters = map[string]emitterStarter{
	"rspec":               rubyEmitterStarter("ruby/rspec.rb", ruby.SquashByFile),
	"rspec-squashall":     rubyEmitterStarter("ruby/rspec.rb", ruby.SquashAll),
	"rspec-pendantic":     rubyEmitterStarter("ruby/rspec.rb", ruby.SquashNothing),
	"minitest":            rubyEmitterStarter("ruby/minitest.rb", ruby.SquashByFile),
	"minitest-squashall":  rubyEmitterStarter("ruby/minitest.rb", ruby.SquashAll),
	"minitest-pendantic":  rubyEmitterStarter("ruby/minitest.rb", ruby.SquashNothing),
	"test-unit":           rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashByFile),
	"test-unit-squashall": rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashAll),
	"test-unit-pendantic": rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashNothing),
}

var defaultGlobs = map[string]string {
	"rspec": "spec/**/*spec.rb",
	"rspec-squashall": "spec/**/*spec.rb",
	"rspec-pendantic": "spec/**/*spec.rb",
	"minitest": "test/**/test*.rb",
	"minitest-squashall": "test/**/test*.rb",
	"minitest-pendantic": "test/**/test*.rb",
	"test-unit": "test/**/test*.rb",
	"test-unit-squashall": "test/**/test*.rb",
	"test-unit-pendantic": "test/**/test*.rb",
}

func DefaultGlob(name string) string {
	return defaultGlobs[name]
}

func Resolve(
	name string,
	srv *server.Server,
	passthroughConfig map[string](interface{}),
	workerEnvs []map[string]string,
	dir string,
	env map[string]string,
	seed int,
	files []string) (Emitter, error) {
	starter, ok := starters[name]
	if !ok {
		return nil, errors.New("Could not find starter: " + name)
	}

	return starter(srv, passthroughConfig, workerEnvs, dir, env, seed, files)
}
