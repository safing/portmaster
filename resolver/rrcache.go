package resolver

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"

	"github.com/miekg/dns"
)

// RRCache is used to cache DNS data
//nolint:maligned // TODO
type RRCache struct {
	sync.Mutex

	Domain   string   // constant
	Question dns.Type // constant

	Answer []dns.RR // might be mixed
	Ns     []dns.RR // constant
	Extra  []dns.RR // constant
	TTL    int64    // constant

	Server      string // constant
	ServerScope int8   // constant

	servedFromCache bool     // mutable
	requestingNew   bool     // mutable
	Filtered        bool     // mutable
	FilteredEntries []string // mutable

	updated int64 // mutable
}

// ID returns the ID of the RRCache consisting of the domain and question type.
func (rrCache *RRCache) ID() string {
	return rrCache.Domain + rrCache.Question.String()
}

// Expired returns whether the record has expired.
func (rrCache *RRCache) Expired() bool {
	return rrCache.TTL <= time.Now().Unix()
}

// MixAnswers randomizes the answer records to allow dumb clients (who only look at the first record) to reliably connect.
func (rrCache *RRCache) MixAnswers() {
	rrCache.Lock()
	defer rrCache.Unlock()

	for i := range rrCache.Answer {
		j := rand.Intn(i + 1)
		rrCache.Answer[i], rrCache.Answer[j] = rrCache.Answer[j], rrCache.Answer[i]
	}
}

// Clean sets all TTLs to 17 and sets cache expiry with specified minimum.
func (rrCache *RRCache) Clean(minExpires uint32) {
	var lowestTTL uint32 = 0xFFFFFFFF
	var header *dns.RR_Header

	// set TTLs to 17
	// TODO: double append? is there something more elegant?
	for _, rr := range append(rrCache.Answer, append(rrCache.Ns, rrCache.Extra...)...) {
		header = rr.Header()
		if lowestTTL > header.Ttl {
			lowestTTL = header.Ttl
		}
		header.Ttl = 17
	}

	// TTL range limits
	switch {
	case lowestTTL < minExpires:
		lowestTTL = minExpires
	case lowestTTL > maxTTL:
		lowestTTL = maxTTL
	}

	// shorten caching
	switch {
	case rrCache.IsNXDomain():
		// NXDomain
		lowestTTL = 10
	case netenv.IsConnectivityDomain(rrCache.Domain):
		// Responses from these domains might change very quickly depending on the environment.
		lowestTTL = 3
	case !netenv.Online():
		// Not being fully online could mean that we get funny responses.
		lowestTTL = 60
	}

	// log.Tracef("lowest TTL is %d", lowestTTL)
	rrCache.TTL = time.Now().Unix() + int64(lowestTTL)
}

// ExportAllARecords return of a list of all A and AAAA IP addresses.
func (rrCache *RRCache) ExportAllARecords() (ips []net.IP) {
	for _, rr := range rrCache.Answer {
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
func (rrCache *RRCache) ToNameRecord() *NameRecord {
	new := &NameRecord{
		Domain:      rrCache.Domain,
		Question:    rrCache.Question.String(),
		TTL:         rrCache.TTL,
		Server:      rrCache.Server,
		ServerScope: rrCache.ServerScope,
	}

	// stringify RR entries
	for _, entry := range rrCache.Answer {
		new.Answer = append(new.Answer, entry.String())
	}
	for _, entry := range rrCache.Ns {
		new.Ns = append(new.Ns, entry.String())
	}
	for _, entry := range rrCache.Extra {
		new.Extra = append(new.Extra, entry.String())
	}

	return new
}

// Save saves the RRCache to the database as a NameRecord.
func (rrCache *RRCache) Save() error {
	return rrCache.ToNameRecord().Save()
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
		rrCache.Answer = parseRR(rrCache.Answer, entry)
	}
	for _, entry := range nameRecord.Ns {
		rrCache.Ns = parseRR(rrCache.Ns, entry)
	}
	for _, entry := range nameRecord.Extra {
		rrCache.Extra = parseRR(rrCache.Extra, entry)
	}

	rrCache.Server = nameRecord.Server
	rrCache.ServerScope = nameRecord.ServerScope
	rrCache.servedFromCache = true
	return rrCache, nil
}

func parseRR(section []dns.RR, entry string) []dns.RR {
	rr, err := dns.NewRR(entry)
	switch {
	case err != nil:
		log.Warningf("resolver: failed to parse cached record %q: %s", entry, err)
	case rr == nil:
		log.Warningf("resolver: failed to parse cached record %q: resulted in nil record", entry)
	default:
		return append(section, rr)
	}
	return section
}

// ServedFromCache marks the RRCache as served from cache.
func (rrCache *RRCache) ServedFromCache() bool {
	return rrCache.servedFromCache
}

// RequestingNew informs that it has expired and new RRs are being fetched.
func (rrCache *RRCache) RequestingNew() bool {
	return rrCache.requestingNew
}

// Flags formats ServedFromCache and RequestingNew to a condensed, flag-like format.
func (rrCache *RRCache) Flags() string {
	var s string
	if rrCache.servedFromCache {
		s += "C"
	}
	if rrCache.requestingNew {
		s += "R"
	}
	if rrCache.Filtered {
		s += "F"
	}

	if s != "" {
		return fmt.Sprintf(" [%s]", s)
	}
	return ""
}

// IsNXDomain returnes whether the result is nxdomain.
func (rrCache *RRCache) IsNXDomain() bool {
	return len(rrCache.Answer) == 0
}

// ShallowCopy returns a shallow copy of the cache. slices are not copied, but referenced.
func (rrCache *RRCache) ShallowCopy() *RRCache {
	return &RRCache{
		Domain:   rrCache.Domain,
		Question: rrCache.Question,
		Answer:   rrCache.Answer,
		Ns:       rrCache.Ns,
		Extra:    rrCache.Extra,
		TTL:      rrCache.TTL,

		Server:      rrCache.Server,
		ServerScope: rrCache.ServerScope,

		updated:         rrCache.updated,
		servedFromCache: rrCache.servedFromCache,
		requestingNew:   rrCache.requestingNew,
		Filtered:        rrCache.Filtered,
		FilteredEntries: rrCache.FilteredEntries,
	}
}
