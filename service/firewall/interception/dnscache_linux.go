package interception

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/safing/portmaster/base/log"
)

// flushSystemDNSCache clears the cache of systemd-resolved, like
// "resolvectl flush-caches". Names resolved before the interception started
// are cached there, so apps would reach their IPs without a DNS query that
// Portmaster sees: the connections would miss the requested domain and its
// CNAMEs, and domain rules would not apply to them until the entries expire.
func flushSystemDNSCache() {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Debugf("interception: cannot connect to the system bus to flush the systemd-resolved cache: %s", err)
		return
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1").
		CallWithContext(ctx, "org.freedesktop.resolve1.Manager.FlushCaches", 0).Err
	if err != nil {
		dbusErr, ok := errors.AsType[dbus.Error](err)
		if ok && dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
			// systemd-resolved is not in use.
			return
		}
		log.Warningf("interception: failed to flush the systemd-resolved cache: %s", err)
		return
	}
	log.Info("interception: flushed the systemd-resolved cache")
}
