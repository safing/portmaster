package database

import (
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

// Subscription is a database subscription for updates.
type Subscription struct {
	q        *query.Query
	local    bool
	internal bool

	Feed chan record.Record
}

// Cancel cancels the subscription.
func (s *Subscription) Cancel() error {
	c, err := getController(s.q.DatabaseName())
	if err != nil {
		return err
	}

	c.subscriptionLock.Lock()
	defer c.subscriptionLock.Unlock()

	for key, sub := range c.subscriptions {
		if sub.q == s.q {
			c.subscriptions = append(c.subscriptions[:key], c.subscriptions[key+1:]...)
			close(s.Feed) // this close is guarded by the controllers subscriptionLock.
			return nil
		}
	}
	return nil
}
