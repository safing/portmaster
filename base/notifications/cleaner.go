package notifications

import (
	"context"
	"time"
)

func cleaner(ctx context.Context) error { //nolint:unparam // Conforms to worker interface
	ticker := module.NewSleepyTicker(1*time.Second, 0)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Wait():
			deleteExpiredNotifs()
		}
	}
}

func deleteExpiredNotifs() {
	// Get a copy of the notification map.
	notsCopy := getNotsCopy()

	// Delete all expired notifications.
	for _, n := range notsCopy {
		if n.isExpired() {
			n.delete(true)
		}
	}
}

func (n *Notification) isExpired() bool {
	n.Lock()
	defer n.Unlock()

	return n.Expires > 0 && n.Expires < time.Now().Unix()
}

func getNotsCopy() []*Notification {
	notsLock.RLock()
	defer notsLock.RUnlock()

	notsCopy := make([]*Notification, 0, len(nots))
	for _, n := range nots {
		notsCopy = append(notsCopy, n)
	}

	return notsCopy
}
