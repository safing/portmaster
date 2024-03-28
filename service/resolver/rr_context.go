package resolver

import (
	"time"

	"github.com/miekg/dns"
)

// DNSRequestContext is a static structure to add information to DNS request connections.
type DNSRequestContext struct {
	Domain   string
	Question string
	RCode    string

	ServedFromCache bool
	RequestingNew   bool
	IsBackup        bool
	Filtered        bool

	Modified time.Time
	Expires  time.Time
}

// ToDNSRequestContext returns a new DNSRequestContext of the RRCache.
func (rrCache *RRCache) ToDNSRequestContext() *DNSRequestContext {
	return &DNSRequestContext{
		Domain:   rrCache.Domain,
		Question: rrCache.Question.String(),
		RCode:    dns.RcodeToString[rrCache.RCode],

		ServedFromCache: rrCache.ServedFromCache,
		RequestingNew:   rrCache.RequestingNew,
		IsBackup:        rrCache.IsBackup,
		Filtered:        rrCache.Filtered,

		Modified: time.Unix(rrCache.Modified, 0),
		Expires:  time.Unix(rrCache.Expires, 0),
	}
}
