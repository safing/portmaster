//go:build linux
// +build linux

package dnsmonitor

import (
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/miekg/dns"
	"github.com/varlink/go/varlink"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/resolver"
)

type Listener struct {
	varlinkConn *varlink.Connection
}

func newListener(module *DNSMonitor) (*Listener, error) {
	// Set source of the resolver.
	ResolverInfo.Source = resolver.ServerSourceSystemd

	// Check if the system has systemd-resolver.
	_, err := os.Stat("/run/systemd/resolve/io.systemd.Resolve.Monitor")
	if err != nil {
		return nil, fmt.Errorf("system does not support systemd resolver monitor")
	}

	listener := &Listener{}

	restartAttempts := 0

	module.mgr.Go("systemd-resolver-event-listener", func(w *mgr.WorkerCtx) error {
		// Abort initialization if the connection failed after too many tries.
		if restartAttempts > 10 {
			return nil
		}
		restartAttempts += 1

		// Initialize varlink connection
		varlinkConn, err := varlink.NewConnection(module.mgr.Ctx(), "unix:/run/systemd/resolve/io.systemd.Resolve.Monitor")
		if err != nil {
			return fmt.Errorf("failed to connect to systemd-resolver varlink service: %w", err)
		}
		defer func() {
			if varlinkConn != nil {
				err = varlinkConn.Close()
				if err != nil {
					log.Errorf("dnsmonitor: failed to close varlink connection: %s", err)
				}
			}
		}()

		listener.varlinkConn = varlinkConn
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

			// Ignore if there is no question.
			if queryResult.Question == nil || len(*queryResult.Question) == 0 {
				continue
			}

			// Protmaster self check
			domain := (*queryResult.Question)[0].Name
			if processIfSelfCheckDomain(dns.Fqdn(domain)) {
				// Not need to process result.
				continue
			}

			if queryResult.Rcode != nil {
				continue // Ignore DNS errors
			}

			listener.processAnswer(domain, &queryResult)
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
	return nil
}

func (l *Listener) processAnswer(domain string, queryResult *QueryResult) {
	// Allocated data struct for the parsed result.
	cnames := make(map[string]string)
	ips := make([]net.IP, 0, 5)

	// Check if the query is valid
	if queryResult.Answer == nil {
		return
	}

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

	saveDomain(domain, ips, cnames, resolver.IPInfoProfileScopeGlobal)
}
