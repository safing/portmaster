package resolver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/nameserver/nsutil"
	"github.com/safing/portmaster/service/netenv"
)

// RRCache is a single-use structure to hold a DNS response.
// Persistence is handled through NameRecords because of a limitation of the
// underlying dns library.
//
//nolint:maligned
type RRCache struct {
	// Respnse Header
	Domain   string
	Question dns.Type
	RCode    int

	// Response Content
	Answer  []dns.RR `json:"-"`
	Ns      []dns.RR `json:"-"`
	Extra   []dns.RR `json:"-"`
	Expires int64

	// Resolver Information
	Resolver *ResolverInfo `json:"-"`

	// Metadata about the request and handling
	ServedFromCache bool
	RequestingNew   bool
	IsBackup        bool
	Filtered        bool
	FilteredEntries []string

	// Modified holds when this entry was last changed, ie. saved to database.
	// This field is only populated when the entry comes from the cache.
	Modified int64
}

// ID returns the ID of the RRCache consisting of the domain and question type.
func (rrCache *RRCache) ID() string {
	return rrCache.Domain + rrCache.Question.String()
}

// Expired returns whether the record has expired.
func (rrCache *RRCache) Expired() bool {
	return rrCache.Expires <= time.Now().Unix()
}

// ExpiresSoon returns whether the record will expire soon (or already has) and
// should already be refreshed.
func (rrCache *RRCache) ExpiresSoon() bool {
	return rrCache.Expires <= time.Now().Unix()+refreshTTL
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
	case rrCache.RCode != dns.RcodeSuccess:
		// Any sort of error.
		lowestTTL = 10
	case netenv.IsConnectivityDomain(rrCache.Domain):
		// Responses from these domains might change very quickly depending on the environment.
		lowestTTL = 3
	case len(rrCache.Answer) == 0:
		// Empty answer section: Domain exists, but not the queried RR.
		lowestTTL = 60
	case !netenv.Online():
		// Not being fully online could mean that we get funny responses.
		lowestTTL = 60
	}

	// log.Tracef("lowest TTL is %d", lowestTTL)
	rrCache.Expires = time.Now().Unix() + int64(lowestTTL)
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
	newRecord := &NameRecord{
		Domain:   rrCache.Domain,
		Question: rrCache.Question.String(),
		RCode:    rrCache.RCode,
		Expires:  rrCache.Expires,
		Resolver: rrCache.Resolver,
	}

	// Serialize RR entries to strings.
	newRecord.Answer = toNameRecordSection(rrCache.Answer)
	newRecord.Ns = toNameRecordSection(rrCache.Ns)
	newRecord.Extra = toNameRecordSection(rrCache.Extra)

	return newRecord
}

func toNameRecordSection(rrSection []dns.RR) []string {
	serialized := make([]string, 0, len(rrSection))
	for _, entry := range rrSection {
		// Ignore some RR types.
		switch entry.Header().Rrtype {
		case dns.TypeOPT:
			// This record type cannot be unserialized again and only consists of
			// additional metadata.
		case dns.TypeNULL:
		default:
			serialized = append(serialized, entry.String())
		}
	}
	return serialized
}

// rcodeIsCacheable returns whether a record with the given RCode should be cached.
func rcodeIsCacheable(rCode int) bool {
	switch rCode {
	case dns.RcodeSuccess, dns.RcodeNameError, dns.RcodeRefused:
		return true
	default:
		return false
	}
}

// Cacheable returns whether the record should be cached.
func (rrCache *RRCache) Cacheable() bool {
	return rcodeIsCacheable(rrCache.RCode)
}

// Save saves the RRCache to the database as a NameRecord.
func (rrCache *RRCache) Save() error {
	if !rrCache.Cacheable() {
		return nil
	}

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

	rrCache.RCode = nameRecord.RCode
	rrCache.Expires = nameRecord.Expires
	for _, entry := range nameRecord.Answer {
		rrCache.Answer = parseRR(rrCache.Answer, entry)
	}
	for _, entry := range nameRecord.Ns {
		rrCache.Ns = parseRR(rrCache.Ns, entry)
	}
	for _, entry := range nameRecord.Extra {
		rrCache.Extra = parseRR(rrCache.Extra, entry)
	}

	rrCache.Resolver = nameRecord.Resolver
	rrCache.ServedFromCache = true
	rrCache.Modified = nameRecord.Meta().Modified
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

// Flags formats ServedFromCache and RequestingNew to a condensed, flag-like format.
func (rrCache *RRCache) Flags() string {
	var s string
	if rrCache.ServedFromCache {
		s += "C"
	}
	if rrCache.RequestingNew {
		s += "R"
	}
	if rrCache.IsBackup {
		s += "B"
	}
	if rrCache.Filtered {
		s += "F"
	}

	if s != "" {
		return fmt.Sprintf(" [%s]", s)
	}
	return ""
}

// ShallowCopy returns a shallow copy of the cache. slices are not copied, but referenced.
func (rrCache *RRCache) ShallowCopy() *RRCache {
	return &RRCache{
		Domain:   rrCache.Domain,
		Question: rrCache.Question,
		RCode:    rrCache.RCode,

		Answer:  rrCache.Answer,
		Ns:      rrCache.Ns,
		Extra:   rrCache.Extra,
		Expires: rrCache.Expires,

		Resolver: rrCache.Resolver,

		ServedFromCache: rrCache.ServedFromCache,
		RequestingNew:   rrCache.RequestingNew,
		IsBackup:        rrCache.IsBackup,
		Filtered:        rrCache.Filtered,
		FilteredEntries: rrCache.FilteredEntries,
		Modified:        rrCache.Modified,
	}
}

// ReplaceAnswerNames is a helper function that replaces all answer names, that
// match the query domain, with another value. This is used to support handling
// non-standard query names, which are resolved normalized, but have to be
// reverted back for the origin non-standard query name in order for the
// clients to recognize the response.
func (rrCache *RRCache) ReplaceAnswerNames(fqdn string) {
	for _, answer := range rrCache.Answer {
		if answer.Header().Name == rrCache.Domain {
			answer.Header().Name = fqdn
		}
	}
}

// ReplyWithDNS creates a new reply to the given query with the data from the
// RRCache, and additional informational records.
func (rrCache *RRCache) ReplyWithDNS(ctx context.Context, request *dns.Msg) *dns.Msg {
	// reply to query
	reply := new(dns.Msg)
	reply.SetRcode(request, rrCache.RCode)
	reply.Answer = rrCache.Answer
	reply.Ns = rrCache.Ns
	reply.Extra = rrCache.Extra

	return reply
}

// GetExtraRRs returns a slice of RRs with additional informational records.
func (rrCache *RRCache) GetExtraRRs(ctx context.Context, query *dns.Msg) (extra []dns.RR) {
	// Add cache status and source of data.
	if rrCache.ServedFromCache {
		extra = addExtra(ctx, extra, "served from cache, resolved by "+rrCache.Resolver.DescriptiveName())
	} else {
		extra = addExtra(ctx, extra, "freshly resolved by "+rrCache.Resolver.DescriptiveName())
	}

	// Add expiry and cache information.
	switch {
	case rrCache.Expires == 0:
		extra = addExtra(ctx, extra, "record does not expire")
	case rrCache.Expired():
		extra = addExtra(ctx, extra, fmt.Sprintf("record expired since %s", time.Since(time.Unix(rrCache.Expires, 0)).Round(time.Second)))
	default:
		extra = addExtra(ctx, extra, fmt.Sprintf("record valid for %s", time.Until(time.Unix(rrCache.Expires, 0)).Round(time.Second)))
	}
	if rrCache.RequestingNew {
		extra = addExtra(ctx, extra, "async request to refresh the cache has been started")
	}
	if rrCache.IsBackup {
		extra = addExtra(ctx, extra, "this record is served because a fresh request was unsuccessful")
	}

	// Add information about filtered entries.
	if rrCache.Filtered {
		if len(rrCache.FilteredEntries) > 1 {
			extra = addExtra(ctx, extra, fmt.Sprintf("%d RRs have been filtered:", len(rrCache.FilteredEntries)))
		} else {
			extra = addExtra(ctx, extra, fmt.Sprintf("%d RR has been filtered:", len(rrCache.FilteredEntries)))
		}
		for _, filteredRecord := range rrCache.FilteredEntries {
			extra = addExtra(ctx, extra, filteredRecord)
		}
	}

	return extra
}

func addExtra(ctx context.Context, extra []dns.RR, msg string) []dns.RR {
	rr, err := nsutil.MakeMessageRecord(log.InfoLevel, msg)
	if err != nil {
		log.Tracer(ctx).Warningf("resolver: failed to add informational record to reply: %s", err)
		return extra
	}
	extra = append(extra, rr)
	return extra
}
