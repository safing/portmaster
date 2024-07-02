package query

import "testing"

func TestGetOpName(t *testing.T) {
	t.Parallel()

	if getOpName(254) != "[unknown]" {
		t.Error("unexpected output")
	}
}
