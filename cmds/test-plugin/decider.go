package main

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type TestDeciderPlugin struct{}

func (TestDeciderPlugin) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	return proto.Verdict_VERDICT_UNDECIDED, "", nil
}
