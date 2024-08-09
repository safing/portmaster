package record

import "time"

// Meta holds metadata about the record.
type Meta struct {
	Created   int64
	Modified  int64
	Expires   int64
	Deleted   int64
	secret    bool // secrets must not be sent to the UI, only synced between nodes
	cronjewel bool // crownjewels must never leave the instance, but may be read by the UI
}

// SetAbsoluteExpiry sets an absolute expiry time (in seconds), that is not affected when the record is updated.
func (m *Meta) SetAbsoluteExpiry(seconds int64) {
	m.Expires = seconds
	m.Deleted = 0
}

// SetRelativateExpiry sets a relative expiry time (ie. TTL in seconds) that is automatically updated whenever the record is updated/saved.
func (m *Meta) SetRelativateExpiry(seconds int64) {
	if seconds >= 0 {
		m.Deleted = -seconds
	}
}

// GetAbsoluteExpiry returns the absolute expiry time.
func (m *Meta) GetAbsoluteExpiry() int64 {
	return m.Expires
}

// GetRelativeExpiry returns the current relative expiry time - ie. seconds until expiry.
// A negative value signifies that the record does not expire.
func (m *Meta) GetRelativeExpiry() int64 {
	if m.Expires == 0 {
		return -1
	}

	abs := m.Expires - time.Now().Unix()
	if abs < 0 {
		return 0
	}
	return abs
}

// MakeCrownJewel marks the database records as a crownjewel, meaning that it will not be sent/synced to other devices.
func (m *Meta) MakeCrownJewel() {
	m.cronjewel = true
}

// MakeSecret sets the database record as secret, meaning that it may only be used internally, and not by interfacing processes, such as the UI.
func (m *Meta) MakeSecret() {
	m.secret = true
}

// Update updates the internal meta states and should be called before writing the record to the database.
func (m *Meta) Update() {
	now := time.Now().Unix()
	m.Modified = now
	if m.Created == 0 {
		m.Created = now
	}
	if m.Deleted < 0 {
		m.Expires = now - m.Deleted
	}
}

// Reset resets all metadata, except for the secret and crownjewel status.
func (m *Meta) Reset() {
	m.Created = 0
	m.Modified = 0
	m.Expires = 0
	m.Deleted = 0
}

// Delete marks the record as deleted.
func (m *Meta) Delete() {
	m.Deleted = time.Now().Unix()
}

// IsDeleted returns whether the record is deleted.
func (m *Meta) IsDeleted() bool {
	return m.Deleted > 0
}

// CheckValidity checks whether the database record is valid.
func (m *Meta) CheckValidity() (valid bool) {
	if m == nil {
		return false
	}

	switch {
	case m.Deleted > 0:
		return false
	case m.Expires > 0 && m.Expires < time.Now().Unix():
		return false
	default:
		return true
	}
}

// CheckPermission checks whether the database record may be accessed with the following scope.
func (m *Meta) CheckPermission(local, internal bool) (permitted bool) {
	if m == nil {
		return false
	}

	switch {
	case !local && m.cronjewel:
		return false
	case !internal && m.secret:
		return false
	default:
		return true
	}
}

// Duplicate returns a new copy of Meta.
func (m *Meta) Duplicate() *Meta {
	return &Meta{
		Created:   m.Created,
		Modified:  m.Modified,
		Expires:   m.Expires,
		Deleted:   m.Deleted,
		secret:    m.secret,
		cronjewel: m.cronjewel,
	}
}
