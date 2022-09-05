package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/safing/portmaster/plugin/shared/proto"
)

func TestReporterIsCalled() error {
	ch := make(chan struct{}, 1)

	reporter.SetHandler(func(c *proto.Connection) {
		ch <- struct{}{}

	}, "example.com.")
	defer reporter.SetHandler(nil)

	go http.Get("https://example.com")

	select {
	case <-ch:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("reporter not called for connection")
	}
}

func TestDeciderIsCalled() error {
	ch := make(chan struct{}, 1)

	decider.SetHandler(func(c *proto.Connection) (proto.Verdict, error) {
		ch <- struct{}{}

		return proto.Verdict_VERDICT_ACCEPT, nil
	}, "example.com.")
	defer decider.SetHandler(nil)

	go http.Get("https://example.com")

	select {
	case <-ch:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("decider not called for connection")
	}
}

func TestBlockingDecider() error {
	decider.SetHandler(func(c *proto.Connection) (proto.Verdict, error) {
		// time.Sleep(1 * time.Second)
		return proto.Verdict_VERDICT_DROP, nil
	}, "example.com.")

	defer decider.SetHandler(nil)

	connReporter := make(chan proto.Verdict, 1)

	reporter.SetHandler(func(c *proto.Connection) {
		if c.Type == proto.ConnectionType_CONNECTION_TYPE_IP {
			connReporter <- c.Verdict
		}
	}, "example.com.")
	defer reporter.SetHandler(nil)

	go http.Get("https://example.com")

	select {
	case verdict := <-connReporter:
		if verdict == proto.Verdict_VERDICT_DROP {
			return fmt.Errorf("blocking decider should have been ingored")
		}

		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("test timeout")
	}
}
