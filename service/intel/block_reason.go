package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/nameserver/nsutil"
)

// ListMatch represents an entity that has been
// matched against filterlists.
type ListMatch struct {
	Entity        string
	ActiveLists   []string
	InactiveLists []string
}

func (lm *ListMatch) String() string {
	inactive := ""
	if len(lm.InactiveLists) > 0 {
		inactive = " and in deactivated lists " + strings.Join(lm.InactiveLists, ", ")
	}
	return fmt.Sprintf(
		"%s in activated lists %s%s",
		lm.Entity,
		strings.Join(lm.ActiveLists, ","),
		inactive,
	)
}

// ListBlockReason is a list of list matches.
type ListBlockReason []ListMatch

func (br ListBlockReason) String() string {
	if len(br) == 0 {
		return ""
	}

	matches := make([]string, len(br))
	for idx, lm := range br {
		matches[idx] = lm.String()
	}

	return strings.Join(matches, " and ")
}

// Context returns br wrapped into a map. It implements
// the endpoints.Reason interface.
func (br ListBlockReason) Context() interface{} {
	return br
}

// MarshalJSON marshals the list block reason into a map
// prefixed with filterlists.
func (br ListBlockReason) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		// we convert to []ListMatch to avoid recursing
		// here.
		"filterlists": []ListMatch(br),
	})
}

// GetExtraRRs implements the nsutil.RRProvider interface
// and adds additional TXT records justifying the reason
// the request was blocked.
func (br ListBlockReason) GetExtraRRs(ctx context.Context, _ *dns.Msg) []dns.RR {
	rrs := make([]dns.RR, 0, len(br))

	for _, lm := range br {
		blockedBy, err := nsutil.MakeMessageRecord(log.InfoLevel, fmt.Sprintf(
			"%s is blocked by filter lists %s",
			lm.Entity,
			strings.Join(lm.ActiveLists, ", "),
		))
		if err == nil {
			rrs = append(rrs, blockedBy)
		} else {
			log.Tracer(ctx).Errorf("intel: failed to create TXT RR for block reason: %s", err)
		}

		if len(lm.InactiveLists) > 0 {
			wouldBeBlockedBy, err := nsutil.MakeMessageRecord(log.InfoLevel, fmt.Sprintf(
				"%s would be blocked by filter lists %s",
				lm.Entity,
				strings.Join(lm.InactiveLists, ", "),
			))
			if err == nil {
				rrs = append(rrs, wouldBeBlockedBy)
			} else {
				log.Tracer(ctx).Errorf("intel: failed to create TXT RR for block reason: %s", err)
			}
		}
	}

	return rrs
}

var _ nsutil.RRProvider = ListBlockReason(nil)
