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
	EnumerateTests(seed int) ([]tapjio.TraceEvent, []runner.TestRunner, error)
}

type emitterStarter func(srv *server.Server, seed int, files []string) (Emitter, error)

var rubyTraceProbes = []string{
	"Kernel#require(path)",
	"Kernel#load",
	"ActiveRecord::ConnectionAdapters::Mysql2Adapter#execute(sql,name)",
	"ActiveRecord::ConnectionAdapters::PostgresSQLAdapter#execute_and_clear(sql,name,binds)",
	"ActiveSupport::Dependencies::Loadable#require(path)",
	"ActiveRecord::ConnectionAdapters::QueryCache#clear_query_cache",
	"ActiveRecord::ConnectionAdapters::SchemaCache#initialize",
	"ActiveRecord::ConnectionAdapters::SchemaCache#clear!",
	"ActiveRecord::ConnectionAdapters::SchemaCache#clear_table_cache!",
}

func rubyEmitterStarter(runnerAssetName string, policy ruby.SquashPolicy) emitterStarter {
	return func(srv *server.Server, seed int, files []string) (Emitter, error) {
		context := ruby.NewRubyContext(seed, runnerAssetName, rubyTraceProbes, srv)
		context.SquashPolicy(policy)
		err := context.Start(files)
		if err != nil {
			return nil, err
		}

		return context, nil
	}
}

var starters = map[string]emitterStarter{
	"rspec": rubyEmitterStarter("ruby/rspec.rb", ruby.SquashByFile),
	"rspec-squashall": rubyEmitterStarter("ruby/rspec.rb", ruby.SquashAll),
	"rspec-pendantic": rubyEmitterStarter("ruby/rspec.rb", ruby.SquashNothing),
	"minitest": rubyEmitterStarter("ruby/minitest.rb", ruby.SquashByFile),
	"minitest-squashall": rubyEmitterStarter("ruby/minitest.rb", ruby.SquashAll),
	"minitest-pendantic": rubyEmitterStarter("ruby/minitest.rb", ruby.SquashNothing),
	"test-unit": rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashByFile),
	"test-unit-squashall": rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashAll),
	"test-unit-pendantic": rubyEmitterStarter("ruby/test-unit.rb", ruby.SquashNothing),
}

func Resolve(name string, srv *server.Server, seed int, files []string) (Emitter, error) {
	starter, ok := starters[name]
	if !ok {
		return nil, errors.New("Could not find starter: " + name)
	}

	return starter(srv, seed, files)
}
