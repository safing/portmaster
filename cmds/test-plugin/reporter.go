package main

import (
	"context"
	"sync"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type ReportHandlerFunc func(*proto.Connection)

type TestReporterPlugin struct {
	l        sync.Mutex
	reporter ReportHandlerFunc

	domainFilter []string
}

func (r *TestReporterPlugin) SetHandler(fn ReportHandlerFunc, domains ...string) {
	r.l.Lock()
	defer r.l.Unlock()

	r.reporter = fn
	r.domainFilter = domains
}

func (r *TestReporterPlugin) ReportConnection(ctx context.Context, conn *proto.Connection) error {
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
