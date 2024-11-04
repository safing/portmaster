package dnslistener

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
	"github.com/varlink/go/varlink"
)

var ResolverInfo = resolver.ResolverInfo{
	Name:   "SystemdResolver",
	Type:   "env",
	Source: "System",
}

type DNSListener struct {
	instance instance
	mgr      *mgr.Manager

	varlinkConn *varlink.Connection
}

func (dl *DNSListener) Manager() *mgr.Manager {
	return dl.mgr
}

func (dl *DNSListener) Start() error {
	var err error

	// Create the varlink connection with the systemd resolver.
	dl.varlinkConn, err = varlink.NewConnection(dl.mgr.Ctx(), "unix:/run/systemd/resolve/io.systemd.Resolve.Monitor")
	if err != nil {
		log.Errorf("dnslistener: failed to connect to systemd-resolver varlink service: %s", err)
		return nil
	}

	dl.mgr.Go("systemd-resolver-event-listener", func(w *mgr.WorkerCtx) error {
		// Subscribe to the dns query events
		receive, err := dl.varlinkConn.Send(dl.mgr.Ctx(), "io.systemd.Resolve.Monitor.SubscribeQueryResults", nil, varlink.More)
		if err != nil {
			if varlinkErr, ok := err.(*varlink.Error); ok {
				return fmt.Errorf("failed to issue Varlink call: %+v", varlinkErr.Parameters)
			} else {
				return fmt.Errorf("failed to issue Varlink call: %v", err)
			}
		}

		for {
			queryResult := QueryResult{}
			// Receive the next event from the resolver.
			flags, err := receive(w.Ctx(), &queryResult)
			if err != nil {
				if varlinkErr, ok := err.(*varlink.Error); ok {
					return fmt.Errorf("failed to receive Varlink reply: %+v", varlinkErr.Parameters)
				} else {
					return fmt.Errorf("failed to receive Varlink reply: %v", err)
				}
			}

			// Check if the reply indicates the end of the stream
			if flags&varlink.Continues == 0 {
				break
			}

			if queryResult.Rcode != nil {
				continue // Ignore DNS errors
			}

			dl.processAnswer(&queryResult)

		}

		return nil
	})

	return nil
}

func (dl *DNSListener) processAnswer(queryResult *QueryResult) {
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

	for _, ip := range ips {
		// Never save domain attributions for localhost IPs.
		if netutils.GetIPScope(ip) == netutils.HostLocal {
			continue
		}
		fqdn := dns.Fqdn(domain)

		// Create new record for this IP.
		record := resolver.ResolvedDomain{
			Domain:            fqdn,
			Resolver:          &ResolverInfo,
			DNSRequestContext: &resolver.DNSRequestContext{},
			Expires:           0,
		}

		for {
			nextDomain, isCNAME := cnames[domain]
			if !isCNAME {
				break
			}

			record.CNAMEs = append(record.CNAMEs, nextDomain)
			domain = nextDomain
		}

		info := resolver.IPInfo{
			IP: ip.String(),
		}

		// Add the new record to the resolved domains for this IP and scope.
		info.AddDomain(record)

		// Save if the record is new or has been updated.
		if err := info.Save(); err != nil {
			log.Errorf("nameserver: failed to save IP info record: %s", err)
		}
	}
}

func (dl *DNSListener) Stop() error {
	if dl.varlinkConn != nil {
		_ = dl.varlinkConn.Close()
	}
	return nil
}

var shimLoaded atomic.Bool

func New(instance instance) (*DNSListener, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("DNSListener")
	module := &DNSListener{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
