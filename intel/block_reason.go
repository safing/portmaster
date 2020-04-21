package intel

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
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

// ToRRs returns a set of dns TXT records that describe the
// block reason.
func (br ListBlockReason) ToRRs() []dns.RR {
	rrs := make([]dns.RR, 0, len(br))

	for _, lm := range br {
		blockedBy, err := dns.NewRR(fmt.Sprintf(
			"%s-blockedBy.		0	IN	TXT 	%q",
			strings.TrimRight(lm.Entity, "."),
			strings.Join(lm.ActiveLists, ","),
		))
		if err == nil {
			rrs = append(rrs, blockedBy)
		} else {
			log.Errorf("intel: failed to create TXT RR for block reason: %s", err)
		}

		if len(lm.InactiveLists) > 0 {
			wouldBeBlockedBy, err := dns.NewRR(fmt.Sprintf(
				"%s-wouldBeBlockedBy.		0	IN	TXT 	%q",
				strings.TrimRight(lm.Entity, "."),
				strings.Join(lm.ActiveLists, ","),
			))
			if err == nil {
				rrs = append(rrs, wouldBeBlockedBy)
			} else {
				log.Errorf("intel: failed to create TXT RR for block reason: %s", err)
			}
		}
	}

	return rrs
}
