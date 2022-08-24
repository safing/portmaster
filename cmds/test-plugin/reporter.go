package main

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type TestReporterPlugin struct{}

func (TestReporterPlugin) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	return nil
}
