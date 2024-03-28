package endpoints

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/safing/portmaster/service/intel"
)

// Endpoints is a list of permitted or denied endpoints.
type Endpoints []Endpoint

// EPResult represents the result of a check against an EndpointPermission.
type EPResult uint8

// Endpoint matching return values.
const (
	NoMatch EPResult = iota
	MatchError
	Denied
	Permitted
)

// IsDecision returns true if result represents a decision
// and false if result is NoMatch or Undeterminable.
func IsDecision(result EPResult) bool {
	return result == Denied || result == Permitted || result == MatchError
}

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
			return endpoints, fmt.Errorf("encountered %d errors, first was: %w", errCnt, firstErr)
		}
		return endpoints, firstErr
	}

	return endpoints, nil
}

// ListEntryValidationRegex is a regex to bullshit check endpoint list entries.
var ListEntryValidationRegex = strings.Join([]string{
	`^(\+|\-) `,                   // Rule verdict.
	`(! +)?`,                      // Invert matching.
	`[A-z0-9\.:\-*/]+`,            // Entity matching.
	`( `,                          // Start of optional matching.
	`[A-z0-9*]+`,                  // Protocol matching.
	`(/[A-z0-9]+(\-[A-z0-9]+)?)?`, // Port and port range matching.
	`)?`,                          // End of optional matching.
	`( +#.*)?`,                    // Optional comment.
}, "")

// ValidateEndpointListConfigOption validates the given value.
func ValidateEndpointListConfigOption(value interface{}) error {
	list, ok := value.([]string)
	if !ok {
		return errors.New("invalid type")
	}

	_, err := ParseEndpoints(list)
	return err
}

// IsSet returns whether the Endpoints object is "set".
func (e Endpoints) IsSet() bool {
	return len(e) > 0
}

// Match checks whether the given entity matches any of the endpoint definitions in the list.
func (e Endpoints) Match(ctx context.Context, entity *intel.Entity) (result EPResult, reason Reason) {
	for _, entry := range e {
		if entry == nil {
			continue
		}

		if result, reason = entry.Matches(ctx, entity); result != NoMatch {
			return
		}
	}

	return NoMatch, nil
}

// MatchMulti checks whether the given entities match any of the endpoint
// definitions in the list. Every rule is evaluated against all given entities
// and only if not match was registered, the next rule is evaluated.
func (e Endpoints) MatchMulti(ctx context.Context, entities ...*intel.Entity) (result EPResult, reason Reason) {
	for _, entry := range e {
		if entry == nil {
			continue
		}

		for _, entity := range entities {
			if entity == nil {
				continue
			}

			if result, reason = entry.Matches(ctx, entity); result != NoMatch {
				return
			}
		}
	}

	return NoMatch, nil
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
	case MatchError:
		return "Match Error"
	case Denied:
		return "Denied"
	case Permitted:
		return "Permitted"
	default:
		return "Unknown"
	}
}
