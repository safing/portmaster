package database

import (
	"testing"

	"github.com/safing/portmaster/base/database/query"
)

func TestControllerHasSubscribers(t *testing.T) {
	t.Parallel()

	var nilController *Controller
	if nilController.HasSubscribers("a/b") {
		t.Error("nil controller reports subscribers")
	}

	c := &Controller{}
	if c.HasSubscribers("a/b") {
		t.Error("controller without subscriptions reports subscribers")
	}

	c.subscriptions = append(c.subscriptions,
		&Subscription{q: query.New("db:a/").MustBeValid()},
		&Subscription{q: query.New("db:c/d").MustBeValid()},
	)
	for dbKey, expected := range map[string]bool{
		"a/b": true,
		"a/":  true,
		"a":   false,
		"b/a": false,
		"c/d": true,
		"c/e": false,
	} {
		if c.HasSubscribers(dbKey) != expected {
			t.Errorf("HasSubscribers(%q) is %v, expected %v", dbKey, !expected, expected)
		}
	}

	// A subscription to the whole database receives every record.
	c.subscriptions = append(c.subscriptions, &Subscription{q: query.New("db:").MustBeValid()})
	if !c.HasSubscribers("b/a") {
		t.Error("subscription to the whole database is not reported")
	}
}
