// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"fmt"
	"net"
	"time"

	"github.com/miekg/dns"
)

// RRCache is used to cache DNS data
type RRCache struct {
	Domain   string
	Question dns.Type

	Answer []dns.RR
	Ns     []dns.RR
	Extra  []dns.RR
	TTL    int64

	Server      string
	ServerScope int8

	updated         int64
	servedFromCache bool
	requestingNew   bool
	Filtered        bool
	FilteredEntries []string
}

// Clean sets all TTLs to 17 and sets cache expiry with specified minimum.
func (m *RRCache) Clean(minExpires uint32) {
	var lowestTTL uint32 = 0xFFFFFFFF
	var header *dns.RR_Header

	// set TTLs to 17
	// TODO: double append? is there something more elegant?
	for _, rr := range append(m.Answer, append(m.Ns, m.Extra...)...) {
		header = rr.Header()
		if lowestTTL > header.Ttl {
			lowestTTL = header.Ttl
		}
		header.Ttl = 17
	}

	// TTL must be at least minExpires
	if lowestTTL < minExpires {
		lowestTTL = minExpires
	}

	// log.Tracef("lowest TTL is %d", lowestTTL)
	m.TTL = time.Now().Unix() + int64(lowestTTL)
}

// ExportAllARecords return of a list of all A and AAAA IP addresses.
func (m *RRCache) ExportAllARecords() (ips []net.IP) {
	for _, rr := range m.Answer {
		if rr.Header().Class != dns.ClassINET {
			continue
		}

		switch rr.Header().Rrtype {
		case dns.TypeA:
			aRecord, ok := rr.(*dns.A)
			if ok {
				ips = append(ips, aRecord.A)
			}
		case dns.TypeAAAA:
			aaaaRecord, ok := rr.(*dns.AAAA)
			if ok {
				ips = append(ips, aaaaRecord.AAAA)
			}
		}
	}
	return
}

// ToNameRecord converts the RRCache to a NameRecord for cleaner persistence.
func (m *RRCache) ToNameRecord() *NameRecord {
	new := &NameRecord{
		Domain:      m.Domain,
		Question:    m.Question.String(),
		TTL:         m.TTL,
		Server:      m.Server,
		ServerScope: m.ServerScope,
	}

	// stringify RR entries
	for _, entry := range m.Answer {
		new.Answer = append(new.Answer, entry.String())
	}
	for _, entry := range m.Ns {
		new.Ns = append(new.Ns, entry.String())
	}
	for _, entry := range m.Extra {
		new.Extra = append(new.Extra, entry.String())
	}

	return new
}

// Save saves the RRCache to the database as a NameRecord.
func (m *RRCache) Save() error {
	return m.ToNameRecord().Save()
}

// GetRRCache tries to load the corresponding NameRecord from the database and convert it.
func GetRRCache(domain string, question dns.Type) (*RRCache, error) {
	rrCache := &RRCache{
		Domain:   domain,
		Question: question,
	}

	nameRecord, err := GetNameRecord(domain, question.String())
	if err != nil {
		return nil, err
	}

	rrCache.TTL = nameRecord.TTL
	for _, entry := range nameRecord.Answer {
		rr, err := dns.NewRR(entry)
		if err == nil {
			rrCache.Answer = append(rrCache.Answer, rr)
		}
	}
	for _, entry := range nameRecord.Ns {
		rr, err := dns.NewRR(entry)
		if err == nil {
			rrCache.Ns = append(rrCache.Ns, rr)
		}
	}
	for _, entry := range nameRecord.Extra {
		rr, err := dns.NewRR(entry)
		if err == nil {
			rrCache.Extra = append(rrCache.Extra, rr)
		}
	}

	rrCache.Server = nameRecord.Server
	rrCache.ServerScope = nameRecord.ServerScope
	rrCache.servedFromCache = true
	return rrCache, nil
}

// ServedFromCache marks the RRCache as served from cache.
func (m *RRCache) ServedFromCache() bool {
	return m.servedFromCache
}

// RequestingNew informs that it has expired and new RRs are being fetched.
func (m *RRCache) RequestingNew() bool {
	return m.requestingNew
}

// Flags formats ServedFromCache and RequestingNew to a condensed, flag-like format.
func (m *RRCache) Flags() string {
	var s string
	if m.servedFromCache {
		s += "C"
	}
	if m.requestingNew {
		s += "R"
	}
	if m.Filtered {
		s += "F"
	}

	if s != "" {
		return fmt.Sprintf(" [%s]", s)
	}
	return ""
}

// IsNXDomain returnes whether the result is nxdomain.
func (m *RRCache) IsNXDomain() bool {
	return len(m.Answer) == 0
}

// ShallowCopy returns a shallow copy of the cache. slices are not copied, but referenced.
func (m *RRCache) ShallowCopy() *RRCache {
	return &RRCache{
		Domain:   m.Domain,
		Question: m.Question,
		Answer:   m.Answer,
		Ns:       m.Ns,
		Extra:    m.Extra,
		TTL:      m.TTL,

		Server:      m.Server,
		ServerScope: m.ServerScope,

		updated:         m.updated,
		servedFromCache: m.servedFromCache,
		requestingNew:   m.requestingNew,
		Filtered:        m.Filtered,
		FilteredEntries: m.FilteredEntries,
	}
}
