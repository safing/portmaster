package status

import (
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
)

// Threat represents a threat to the system.
// A threat is basically a notification with strong
// typed EventData. Use the methods expored on Threat
// to manipulate the EventData field and push updates
// of the notification.
// Do not use EventData directly!
type Threat struct {
	*notifications.Notification
}

// ThreatPayload holds threat related information.
type ThreatPayload struct {
	// MitigationLevel holds the recommended security
	// level to mitigate the threat.
	MitigationLevel uint8
	// Started holds the UNIX epoch timestamp in seconds
	// at which the threat has been detected the first time.
	Started int64
	// Ended holds the UNIX epoch timestamp in seconds
	// at which the threat has been detected the last time.
	Ended int64
	// Data may holds threat-specific data.
	Data interface{}
}

// NewThreat returns a new threat. Note that the
// threat only gets published once Publish is called.
//
// Example:
//
//	threat := NewThreat("portscan", "Someone is scanning you").
//		SetData(portscanResult).
//		SetMitigationLevel(SecurityLevelExtreme).
//		Publish()
//
//	Once you're done, delete the threat
//	threat.Delete().Publish()
func NewThreat(id, title, msg string) *Threat {
	t := &Threat{
		Notification: &notifications.Notification{
			EventID:  id,
			Type:     notifications.Warning,
			Title:    title,
			Category: "Threat",
			Message:  msg,
		},
	}

	t.threatData().Started = time.Now().Unix()

	return t
}

// SetData sets the data member of the threat payload.
func (t *Threat) SetData(data interface{}) *Threat {
	t.Lock()
	defer t.Unlock()

	t.threatData().Data = data
	return t
}

// SetMitigationLevel sets the mitigation level of the
// threat data.
func (t *Threat) SetMitigationLevel(lvl uint8) *Threat {
	t.Lock()
	defer t.Unlock()

	t.threatData().MitigationLevel = lvl
	return t
}

// Delete sets the ended timestamp of the threat.
func (t *Threat) Delete() *Threat {
	t.Lock()
	defer t.Unlock()

	t.threatData().Ended = time.Now().Unix()

	return t
}

// Payload returns a copy of the threat payload.
func (t *Threat) Payload() ThreatPayload {
	t.Lock()
	defer t.Unlock()

	return *t.threatData() // creates a copy
}

// Publish publishes the current threat.
// Publish should always be called when changes to
// the threat are recorded.
func (t *Threat) Publish() *Threat {
	data := t.Payload()
	if data.Ended > 0 {
		DeleteMitigationLevel(t.EventID)
	} else {
		SetMitigationLevel(t.EventID, data.MitigationLevel)
	}

	t.Save()

	return t
}

// threatData returns the threat payload associated with this
// threat. If not data has been created yet a new ThreatPayload
// is attached to t and returned. The caller must make sure to
// hold appropriate locks when working with the returned payload.
func (t *Threat) threatData() *ThreatPayload {
	if t.EventData == nil {
		t.EventData = new(ThreatPayload)
	}

	payload, ok := t.EventData.(*ThreatPayload)
	if !ok {
		log.Warningf("unexpected type %T in thread notification payload", t.EventData)
		return new(ThreatPayload)
	}

	return payload
}
