package main

import (
	"context"
	"sync"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type DecideHandleFunc func(*proto.Connection) (proto.Verdict, error)

type TestDeciderPlugin struct {
	l sync.Mutex

	handler       DecideHandleFunc
	filterDomains []string
}

func (d *TestDeciderPlugin) SetHandler(fn DecideHandleFunc, domains ...string) {
	d.l.Lock()
	defer d.l.Unlock()

	d.handler = fn
	d.filterDomains = domains
}

func (d *TestDeciderPlugin) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	d.l.Lock()
	defer d.l.Unlock()

	if d.handler == nil {
		return proto.Verdict_VERDICT_UNDECIDED, "", nil
	}

	domain := conn.GetEntity().GetDomain()

	for _, filter := range d.filterDomains {
		if filter == domain {
			verdict, err := d.handler(conn)
			return verdict, "", err
		}
	}

	return proto.Verdict_VERDICT_UNDECIDED, "", nil
}
