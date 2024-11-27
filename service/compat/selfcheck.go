package compat

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/resolver"
)

var (
	selfcheckLock sync.Mutex

	// SystemIntegrationCheckDstIP is the IP address to send a packet to for the
	// system integration test.
	SystemIntegrationCheckDstIP = net.IPv4(127, 65, 67, 75)
	// SystemIntegrationCheckProtocol is the IP protocol to use for the system
	// integration test.
	SystemIntegrationCheckProtocol = packet.AnyHostInternalProtocol61

	systemIntegrationCheckDialNet      = fmt.Sprintf("ip4:%d", uint8(SystemIntegrationCheckProtocol))
	systemIntegrationCheckDialIP       = SystemIntegrationCheckDstIP.String()
	systemIntegrationCheckPackets      = make(chan packet.Packet, 1)
	systemIntegrationCheckWaitDuration = 45 * time.Second

	// DNSCheckInternalDomainScope is the domain scope to use for dns checks.
	DNSCheckInternalDomainScope = ".self-check." + resolver.InternalSpecialUseDomain
	dnsCheckReceivedDomain      = make(chan string, 1)
	dnsCheckWaitDuration        = 45 * time.Second
	dnsCheckAnswerLock          sync.Mutex
	dnsCheckAnswer              net.IP

	errSelfcheckSkipped = errors.New("self-check skipped")
)

func selfcheck(ctx context.Context) (issue *systemIssue, err error) {
	selfcheckLock.Lock()
	defer selfcheckLock.Unlock()

	// Step 0: Check if self-check makes sense.
	if !netenv.Online() {
		return nil, fmt.Errorf("%w: device is offline or in limited network", errSelfcheckSkipped)
	}

	// Step 1: Check if the system integration sees a packet.

	// Empty recv channel.
	select {
	case <-systemIntegrationCheckPackets:
	case <-ctx.Done():
		return nil, context.Canceled
	default:
	}

	// Send packet.
	conn, err := net.DialTimeout(
		systemIntegrationCheckDialNet,
		systemIntegrationCheckDialIP,
		time.Second,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create system integration conn: %w", err)
	}
	_, err = conn.Write([]byte("PORTMASTER SELF CHECK"))
	if err != nil {
		return nil, fmt.Errorf("failed to send system integration packet: %w", err)
	}

	// Wait for packet.
	select {
	case <-systemIntegrationCheckPackets:
		// Check passed!
		log.Tracer(ctx).Tracef("compat: self-check #1: system integration check passed")
	case <-time.After(systemIntegrationCheckWaitDuration):
		return systemIntegrationIssue, fmt.Errorf("self-check #1: system integration check failed: did not receive test packet after %s", systemIntegrationCheckWaitDuration)
	case <-ctx.Done():
		return nil, context.Canceled
	}

	// Step 2: Check if a DNS request arrives at the nameserver
	// This step necessary also includes some setup for step 3.

	// Generate random subdomain.
	randomSubdomainBytes, err := rng.Bytes(16)
	if err != nil {
		return nil, fmt.Errorf("self-check #2: failed to get random bytes for subdomain check: %w", err)
	}
	randomSubdomain := "a" + strings.ToLower(hex.EncodeToString(randomSubdomainBytes)) + "b"

	// Generate random answer.
	var B, C, D uint64
	B, err = rng.Number(255)
	if err == nil {
		C, err = rng.Number(255)
	}
	if err == nil {
		D, err = rng.Number(255)
	}
	if err != nil {
		return nil, fmt.Errorf("self-check #2: failed to get random number for subdomain check response: %w", err)
	}
	randomAnswer := net.IPv4(127, byte(B), byte(C), byte(D))
	func() {
		dnsCheckAnswerLock.Lock()
		defer dnsCheckAnswerLock.Unlock()
		dnsCheckAnswer = randomAnswer
	}()

	// Setup variables for lookup worker.
	var (
		dnsCheckReturnedIP  net.IP
		dnsCheckLookupError = make(chan error)
	)

	// Empty recv channel.
	select {
	case <-dnsCheckReceivedDomain:
	case <-ctx.Done():
		return nil, context.Canceled
	default:
	}

	// Start worker for the DNS lookup.
	module.mgr.Go("dns check lookup", func(_ *mgr.WorkerCtx) error {
		ips, err := net.LookupIP(randomSubdomain + DNSCheckInternalDomainScope)
		if err == nil && len(ips) > 0 {
			dnsCheckReturnedIP = ips[0]
		}
		select {
		case dnsCheckLookupError <- err:
		case <-time.After(dnsCheckWaitDuration * 2):
		case <-ctx.Done():
		}

		return nil
	})

	// Wait for the resolver to receive the query.
	select {
	case receivedTestDomain := <-dnsCheckReceivedDomain:
		if receivedTestDomain != randomSubdomain {
			return systemCompatOrManualDNSIssue(), fmt.Errorf("self-check #2: dns integration check failed: received unmatching subdomain %q", receivedTestDomain)
		}
	case <-time.After(dnsCheckWaitDuration):
		return systemCompatOrManualDNSIssue(), fmt.Errorf("self-check #2: dns integration check failed: did not receive test query after %s", dnsCheckWaitDuration)
	}
	log.Tracer(ctx).Tracef("compat: self-check #2: dns integration query check passed")

	// Step 3: Have the nameserver respond with random data in the answer section.

	// Check if the resolver is enabled
	if module.instance.Resolver().IsDisabled() {
		// There is no control over the response, there is nothing more that can be checked.
		return nil, nil
	}

	// Wait for the reply from the resolver.
	select {
	case err := <-dnsCheckLookupError:
		if err != nil {
			return systemCompatibilityIssue, fmt.Errorf("self-check #3: dns integration check failed: failed to receive test response: %w", err)
		}
	case <-time.After(dnsCheckWaitDuration):
		return systemCompatibilityIssue, fmt.Errorf("self-check #3: dns integration check failed: did not receive test response after %s", dnsCheckWaitDuration)
	case <-ctx.Done():
		return nil, context.Canceled
	}

	// Check response.
	if !dnsCheckReturnedIP.Equal(randomAnswer) {
		return systemCompatibilityIssue, fmt.Errorf("self-check #3: dns integration check failed: received unmatching response %q", dnsCheckReturnedIP)
	}
	log.Tracer(ctx).Tracef("compat: self-check #3: dns integration response check passed")

	return nil, nil
}

/*

*   Check if the system integration sees a packet:
    *   Send raw IP packet with random content and protocol, report finding to compat module.
        *   use `Dial("ip4:61", "127.65.67.75")`.
        *   Firewall reports back the data seen on `ip4:61` to IP `127.65.67.75`.
        *   If this fails, the system integration is broken. -&gt; Integration Issue
*   Check if a DNS request arrives at the nameserver:
    *   Send A question for `[random-subdomain].self-check.portmaster.home.arpa.`.
    *   Nameserver reports back the data seen.
    *   If this fails, redirection to the nameserver fails.
        *   This means there is another software interfering with DNS. -&gt; Compatibility Issue
*   Have the nameserver respond with random data in the answer section.
    *   Compat provides nameserver with random response data.
    *   Compat module checks if the received data matches.
    *   If this fails, redirection to the nameserver fails.
        *   This means there is another software interfering with DNS on the return path. -&gt; Compatibility Issue
*   DROPPED: If resolvers are reported failing, but we are online:
    *   Send out plain DNS requests to one.one.one.one. and dns.quad9.net via the Go standard lookup and check if the responses are correct.
    *   If not, something is blocking the Portmaster -&gt; Secure DNS Issue
    *   Discuss if this is necessary:
        *   Does this improve from only having a failed TCP connection to the resolver?
        *   Could another program block port 853, but fully leave requests for one.one.one.one. to port 53 alone?

*/
