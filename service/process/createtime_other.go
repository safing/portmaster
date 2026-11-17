//go:build !linux

package process

import "context"

// fastCreateTime is only implemented on Linux: Getting the creation time is
// cheap on other platforms.
func fastCreateTime(_ context.Context, _ int32) (createdAt int64, ok bool) {
	return 0, false
}
