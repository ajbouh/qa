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

var starters = map[string]emitterStarter{
	"rspec": func(srv *server.Server, seed int, files []string) (Emitter, error) {
		rspec := ruby.NewRspecContext(seed, rubyTraceProbes, srv)
		rspec.SquashPolicy(ruby.SquashByFile)
		err := rspec.Start(files)
		if err != nil {
			return nil, err
		}

		return rspec, nil
	},
	"rspec-squashall": func(srv *server.Server, seed int, files []string) (Emitter, error) {
		rspec := ruby.NewRspecContext(seed, rubyTraceProbes, srv)
		err := rspec.Start(files)
		rspec.SquashPolicy(ruby.SquashAll)
		if err != nil {
			return nil, err
		}

		return rspec, nil
	},
	"rspec-pendantic": func(srv *server.Server, seed int, files []string) (Emitter, error) {
		rspec := ruby.NewRspecContext(seed, rubyTraceProbes, srv)
		err := rspec.Start(files)
		rspec.SquashPolicy(ruby.SquashNothing)
		if err != nil {
			return nil, err
		}

		return rspec, nil
	},
}

func Resolve(name string, srv *server.Server, seed int, files []string) (Emitter, error) {
	starter, ok := starters[name]
	if !ok {
		return nil, errors.New("Could not find starter: " + name)
	}

	return starter(srv, seed, files)
}
