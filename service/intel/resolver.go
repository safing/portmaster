package intel

import (
	"context"
)

var reverseResolver func(ctx context.Context, ip string) (domain string, err error)

// SetReverseResolver allows the resolver module to register a function to allow reverse resolving IPs to domains.
func SetReverseResolver(fn func(ctx context.Context, ip string) (domain string, err error)) {
	if reverseResolver == nil {
		reverseResolver = fn
	}
}
