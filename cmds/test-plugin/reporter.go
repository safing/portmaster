package main

import (
	"context"
	"sync"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type reportHandlerFunc func(*proto.Connection)

type testReporterPlugin struct {
	l        sync.Mutex
	reporter reportHandlerFunc

	domainFilter []string
}

func (r *testReporterPlugin) setHandler(fn reportHandlerFunc, domains ...string) {
	r.l.Lock()
	defer r.l.Unlock()

	r.reporter = fn
	r.domainFilter = domains
}

func (r *testReporterPlugin) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	r.l.Lock()
	defer r.l.Unlock()

	if r.reporter == nil {
		return nil
	}

	domain := conn.GetEntity().GetDomain()
	for _, filter := range r.domainFilter {
		if filter == domain {
			r.reporter(conn)

			return nil
		}
	}
	return nil
}
