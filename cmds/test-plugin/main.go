package main

import (
	"context"

	"github.com/safing/portmaster/plugin/framework"
)

func main() {
	// Create a new decider plugin implementation and register it
	// at the framework
	decider := new(TestDeciderPlugin)
	framework.RegisterDecider(decider)

	// Create a new reporter plugin implementation and register it
	// at the framework
	reporter := new(TestReporterPlugin)
	framework.RegisterReporter(reporter)

	// Once the framework is initialized we can start doing our
	// tests.
	framework.OnInit(func(ctx context.Context) error {
		return nil
	})

	// Finally, actually serve the plugin
	framework.Serve()
}
