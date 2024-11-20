//go:build linux
// +build linux

package dnsmonitor

import (
	"errors"
	"fmt"
	"net"

	"github.com/safing/portmaster/service/mgr"
	"github.com/varlink/go/varlink"
)

type Listener struct {
	varlinkConn *varlink.Connection
}

func newListener(module *DNSMonitor) (*Listener, error) {
	// Create the varlink connection with the systemd resolver.
	varlinkConn, err := varlink.NewConnection(module.mgr.Ctx(), "unix:/run/systemd/resolve/io.systemd.Resolve.Monitor")
	if err != nil {
		return nil, fmt.Errorf("dnsmonitor: failed to connect to systemd-resolver varlink service: %w", err)
	}

	listener := &Listener{varlinkConn: varlinkConn}

	module.mgr.Go("systemd-resolver-event-listener", func(w *mgr.WorkerCtx) error {
		// Subscribe to the dns query events
		receive, err := listener.varlinkConn.Send(w.Ctx(), "io.systemd.Resolve.Monitor.SubscribeQueryResults", nil, varlink.More)
		if err != nil {
			var varlinkErr *varlink.Error
			if errors.As(err, &varlinkErr) {
				return fmt.Errorf("failed to issue Varlink call: %+v", varlinkErr.Parameters)
			} else {
				return fmt.Errorf("failed to issue Varlink call: %w", err)
			}
		}

		for {
			queryResult := QueryResult{}
			// Receive the next event from the resolver.
			flags, err := receive(w.Ctx(), &queryResult)
			if err != nil {
				var varlinkErr *varlink.Error
				if errors.As(err, &varlinkErr) {
					return fmt.Errorf("failed to receive Varlink reply: %+v", varlinkErr.Parameters)
				} else {
					return fmt.Errorf("failed to receive Varlink reply: %w", err)
				}
			}

			// Check if the reply indicates the end of the stream
			if flags&varlink.Continues == 0 {
				break
			}

			if queryResult.Rcode != nil {
				continue // Ignore DNS errors
			}

			listener.processAnswer(&queryResult)
		}
		return nil
	})
	return listener, nil
}

func (l *Listener) flush() error {
	// Nothing to flush
	return nil
}

func (l *Listener) stop() error {
	if l.varlinkConn != nil {
		return l.varlinkConn.Close()
	}
	return nil
}

func (l *Listener) processAnswer(queryResult *QueryResult) {
	// Allocated data struct for the parsed result.
	cnames := make(map[string]string)
	ips := make([]net.IP, 0, 5)

	// Check if the query is valid
	if queryResult.Question == nil || len(*queryResult.Question) == 0 || queryResult.Answer == nil {
		return
	}

	domain := (*queryResult.Question)[0].Name

	// Go trough each answer entry.
	for _, a := range *queryResult.Answer {
		if a.RR.Address != nil {
			ip := net.IP(*a.RR.Address)
			// Answer contains ip address.
			ips = append(ips, ip)

		} else if a.RR.Name != nil {
			// Answer is a CNAME.
			cnames[domain] = *a.RR.Name
		}
	}

	saveDomain(domain, ips, cnames)
}
