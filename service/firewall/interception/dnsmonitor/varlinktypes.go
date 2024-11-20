//go:build linux
// +build linux

package dnsmonitor

// List of struct that define the systemd-resolver varlink dns event protocol.
// Source: `sudo varlinkctl introspect /run/systemd/resolve/io.systemd.Resolve.Monitor io.systemd.Resolve.Monitor`

type ResourceKey struct {
	Class int    `json:"class"`
	Type  int    `json:"type"`
	Name  string `json:"name"`
}

type ResourceRecord struct {
	Key     ResourceKey `json:"key"`
	Name    *string     `json:"name,omitempty"`
	Address *[]byte     `json:"address,omitempty"`
	// Rest of the fields are not used.
	// Priority     *int        `json:"priority,omitempty"`
	// Weight       *int        `json:"weight,omitempty"`
	// Port         *int        `json:"port,omitempty"`
	// CPU          *string     `json:"cpu,omitempty"`
	// OS           *string     `json:"os,omitempty"`
	// Items        *[]string   `json:"items,omitempty"`
	// MName        *string     `json:"mname,omitempty"`
	// RName        *string     `json:"rname,omitempty"`
	// Serial       *int        `json:"serial,omitempty"`
	// Refresh      *int        `json:"refresh,omitempty"`
	// Expire       *int        `json:"expire,omitempty"`
	// Minimum      *int        `json:"minimum,omitempty"`
	// Exchange     *string     `json:"exchange,omitempty"`
	// Version      *int        `json:"version,omitempty"`
	// Size         *int        `json:"size,omitempty"`
	// HorizPre     *int        `json:"horiz_pre,omitempty"`
	// VertPre      *int        `json:"vert_pre,omitempty"`
	// Latitude     *int        `json:"latitude,omitempty"`
	// Longitude    *int        `json:"longitude,omitempty"`
	// Altitude     *int        `json:"altitude,omitempty"`
	// KeyTag       *int        `json:"key_tag,omitempty"`
	// Algorithm    *int        `json:"algorithm,omitempty"`
	// DigestType   *int        `json:"digest_type,omitempty"`
	// Digest       *string     `json:"digest,omitempty"`
	// FPType       *int        `json:"fptype,omitempty"`
	// Fingerprint  *string     `json:"fingerprint,omitempty"`
	// Flags        *int        `json:"flags,omitempty"`
	// Protocol     *int        `json:"protocol,omitempty"`
	// DNSKey       *string     `json:"dnskey,omitempty"`
	// Signer       *string     `json:"signer,omitempty"`
	// TypeCovered  *int        `json:"type_covered,omitempty"`
	// Labels       *int        `json:"labels,omitempty"`
	// OriginalTTL  *int        `json:"original_ttl,omitempty"`
	// Expiration   *int        `json:"expiration,omitempty"`
	// Inception    *int        `json:"inception,omitempty"`
	// Signature    *string     `json:"signature,omitempty"`
	// NextDomain   *string     `json:"next_domain,omitempty"`
	// Types        *[]int      `json:"types,omitempty"`
	// Iterations   *int        `json:"iterations,omitempty"`
	// Salt         *string     `json:"salt,omitempty"`
	// Hash         *string     `json:"hash,omitempty"`
	// CertUsage    *int        `json:"cert_usage,omitempty"`
	// Selector     *int        `json:"selector,omitempty"`
	// MatchingType *int        `json:"matching_type,omitempty"`
	// Data         *string     `json:"data,omitempty"`
	// Tag          *string     `json:"tag,omitempty"`
	// Value        *string     `json:"value,omitempty"`
}

type Answer struct {
	RR      *ResourceRecord `json:"rr,omitempty"`
	Raw     string          `json:"raw"`
	IfIndex *int            `json:"ifindex,omitempty"`
}

type QueryResult struct {
	Ready              *bool          `json:"ready,omitempty"`
	State              *string        `json:"state,omitempty"`
	Rcode              *int           `json:"rcode,omitempty"`
	Errno              *int           `json:"errno,omitempty"`
	Question           *[]ResourceKey `json:"question,omitempty"`
	CollectedQuestions *[]ResourceKey `json:"collectedQuestions,omitempty"`
	Answer             *[]Answer      `json:"answer,omitempty"`
}
