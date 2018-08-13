// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"fmt"
	"net"
	"time"

	"github.com/Safing/safing-core/database"

	datastore "github.com/ipfs/go-datastore"
	"github.com/miekg/dns"
)

// RRCache is used to cache DNS data
type RRCache struct {
	Answer          []dns.RR
	Ns              []dns.RR
	Extra           []dns.RR
	Expires         int64
	Modified        int64
	servedFromCache bool
	requestingNew   bool
}

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

	m.Expires = time.Now().Unix() + int64(lowestTTL)
	m.Modified = time.Now().Unix()

}

func (m *RRCache) ExportAllARecords() (ips []net.IP) {
	for _, rr := range m.Answer {
		if rr.Header().Class == dns.ClassINET && rr.Header().Rrtype == dns.TypeA {
			aRecord, ok := rr.(*dns.A)
			if ok {
				ips = append(ips, aRecord.A)
			}
		} else if rr.Header().Class == dns.ClassINET && rr.Header().Rrtype == dns.TypeAAAA {
			aRecord, ok := rr.(*dns.AAAA)
			if ok {
				ips = append(ips, aRecord.AAAA)
			}
		}
	}
	return
}

func (m *RRCache) ToRRSave() *RRSave {
	var s RRSave
	s.Expires = m.Expires
	s.Modified = m.Modified
	for _, entry := range m.Answer {
		s.Answer = append(s.Answer, entry.String())
	}
	for _, entry := range m.Ns {
		s.Ns = append(s.Ns, entry.String())
	}
	for _, entry := range m.Extra {
		s.Extra = append(s.Extra, entry.String())
	}
	return &s
}

func (m *RRCache) Create(name string) error {
	s := m.ToRRSave()
	return s.CreateObject(&database.DNSCache, name, s)
}

func (m *RRCache) CreateWithType(name string, qtype dns.Type) error {
	s := m.ToRRSave()
	return s.Create(fmt.Sprintf("%s%s", name, qtype.String()))
}

func (m *RRCache) Save() error {
	s := m.ToRRSave()
	return s.SaveObject(s)
}

func GetRRCache(domain string, qtype dns.Type) (*RRCache, error) {
	return GetRRCacheFromNamespace(&database.DNSCache, domain, qtype)
}

func GetRRCacheFromNamespace(namespace *datastore.Key, domain string, qtype dns.Type) (*RRCache, error) {
	var m RRCache

	rrSave, err := GetRRSaveFromNamespace(namespace, domain, qtype)
	if err != nil {
		return nil, err
	}

	m.Expires = rrSave.Expires
	m.Modified = rrSave.Modified
	for _, entry := range rrSave.Answer {
		rr, err := dns.NewRR(entry)
		if err == nil {
			m.Answer = append(m.Answer, rr)
		}
	}
	for _, entry := range rrSave.Ns {
		rr, err := dns.NewRR(entry)
		if err == nil {
			m.Ns = append(m.Ns, rr)
		}
	}
	for _, entry := range rrSave.Extra {
		rr, err := dns.NewRR(entry)
		if err == nil {
			m.Extra = append(m.Extra, rr)
		}
	}

	m.servedFromCache = true
	return &m, nil
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
	switch {
	case m.servedFromCache && m.requestingNew:
		return " [CR]"
	case m.servedFromCache:
		return " [C]"
	case m.requestingNew:
		return " [R]" // theoretically impossible, but let's leave it here, just in case
	default:
		return ""
	}
}

// IsNXDomain returnes whether the result is nxdomain.
func (m *RRCache) IsNXDomain() bool {
	return len(m.Answer) == 0
}

// RRSave is helper struct to RRCache to better save data to the database.
type RRSave struct {
	database.Base
	Answer   []string
	Ns       []string
	Extra    []string
	Expires  int64
	Modified int64
}

var rrSaveModel *RRSave // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(rrSaveModel, func() database.Model { return new(RRSave) })
}

// Create saves RRSave with the provided name in the default namespace.
func (m *RRSave) Create(name string) error {
	return m.CreateObject(&database.DNSCache, name, m)
}

// CreateWithType saves RRSave with the provided name and type in the default namespace.
func (m *RRSave) CreateWithType(name string, qtype dns.Type) error {
	return m.Create(fmt.Sprintf("%s%s", name, qtype.String()))
}

// CreateInNamespace saves RRSave with the provided name in the provided namespace.
func (m *RRSave) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves RRSave.
func (m *RRSave) Save() error {
	return m.SaveObject(m)
}

// GetRRSave fetches RRSave with the provided name in the default namespace.
func GetRRSave(name string, qtype dns.Type) (*RRSave, error) {
	return GetRRSaveFromNamespace(&database.DNSCache, name, qtype)
}

// GetRRSaveFromNamespace fetches RRSave with the provided name in the provided namespace.
func GetRRSaveFromNamespace(namespace *datastore.Key, name string, qtype dns.Type) (*RRSave, error) {
	object, err := database.GetAndEnsureModel(namespace, fmt.Sprintf("%s%s", name, qtype.String()), rrSaveModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*RRSave)
	if !ok {
		return nil, database.NewMismatchError(object, rrSaveModel)
	}
	return model, nil
}
