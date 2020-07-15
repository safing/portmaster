package netenv

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
)

const (
	nameserversRecheck = 2 * time.Second
)

var (
	nameservers        = make([]Nameserver, 0)
	nameserversLock    sync.Mutex
	nameserversExpires = time.Now()
)

// Nameservers returns the currently active nameservers.
func Nameservers() []Nameserver {
	// locking
	nameserversLock.Lock()
	defer nameserversLock.Unlock()
	// cache
	if nameserversExpires.After(time.Now()) {
		return nameservers
	}
	// update cache expiry when finished
	defer func() {
		nameserversExpires = time.Now().Add(nameserversRecheck)
	}()

	// reset
	nameservers = make([]Nameserver, 0)

	// This is a preliminary workaround until we have more time for proper interface using iphlpapi.dll
	// TODO: make nice implementation

	var output = make(chan []byte, 1)
	module.StartWorker("get assigned nameservers", func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, "nslookup", "localhost")
		data, err := cmd.CombinedOutput()
		if err != nil {
			log.Debugf("netenv: failed to get assigned nameserves: %s", err)
			output <- nil
		} else {
			output <- data
		}
		return nil
	})

	select {
	case data := <-output:
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			// check if we found the correct line
			if !strings.HasPrefix(scanner.Text(), "Address:") {
				continue
			}
			// split into fields, check if we have enough fields
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			// parse nameserver, return if valid IP found
			ns := net.ParseIP(fields[1])
			if ns != nil {
				nameservers = append(nameservers, Nameserver{
					IP: ns,
				})
				return nameservers
			}
		}
		log.Debug("netenv: could not find assigned nameserver")
		return nameservers

	case <-time.After(5 * time.Second):
		log.Debug("netenv: timed out while getting assigned nameserves")
	}

	return nameservers
}

// Gateways returns the currently active gateways.
func Gateways() []net.IP {
	return nil
}
