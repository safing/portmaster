package endpoints

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/intel"
)

// Endpoints is a list of permitted or denied endpoints.
type Endpoints []Endpoint

// EPResult represents the result of a check against an EndpointPermission
type EPResult uint8

// Endpoint matching return values
const (
	NoMatch EPResult = iota
	Undeterminable
	Denied
	Permitted
)

// ParseEndpoints parses a list of endpoints and returns a list of Endpoints for matching.
func ParseEndpoints(entries []string) (Endpoints, error) {
	var firstErr error
	var errCnt int
	endpoints := make(Endpoints, 0, len(entries))

entriesLoop:
	for _, entry := range entries {
		ep, err := parseEndpoint(entry)
		if err != nil {
			errCnt++
			if firstErr == nil {
				firstErr = err
			}
			continue entriesLoop
		}

		endpoints = append(endpoints, ep)
	}

	if firstErr != nil {
		if errCnt > 0 {
			return endpoints, fmt.Errorf("encountered %d errors, first was: %s", errCnt, firstErr)
		}
		return endpoints, firstErr
	}

	return endpoints, nil
}

// IsSet returns whether the Endpoints object is "set".
func (e Endpoints) IsSet() bool {
	return len(e) > 0
}

// Match checks whether the given entity matches any of the endpoint definitions in the list.
func (e Endpoints) Match(entity *intel.Entity) (result EPResult, reason string) {
	for _, entry := range e {
		if entry != nil {
			if result, reason = entry.Matches(entity); result != NoMatch {
				return
			}
		}
	}

	return NoMatch, ""
}

func (e Endpoints) String() string {
	s := make([]string, 0, len(e))
	for _, entry := range e {
		s = append(s, entry.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ", "))
}

func (epr EPResult) String() string {
	switch epr {
	case NoMatch:
		return "No Match"
	case Undeterminable:
		return "Undeterminable"
	case Denied:
		return "Denied"
	case Permitted:
		return "Permitted"
	default:
		return "Unknown"
	}
}
