package hub

import (
	"testing"
	"time"

	"github.com/safing/structures/dsd"
)

func FuzzOpenHubMsg(f *testing.F) {
	env, hubID := newTestHubEnvelope(f)
	a := &Announcement{
		ID:         hubID,
		Timestamp:  time.Now().Unix(),
		Name:       "fuzz",
		Transports: []string{"tcp:717"},
	}
	data, err := a.Export(env)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	s := &Status{
		Timestamp: time.Now().Unix(),
		Version:   "1.0.0",
		Lanes:     []*Lane{{ID: hubID, Capacity: 1, Latency: 1}},
	}
	data, err = s.Export(env)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`J{"Signatures":[{"ID":"x"}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _, _ = OpenHubMsg(nil, data, "fuzz-open", true)  //nolint:dogsled
		_, _, _, _ = OpenHubMsg(nil, data, "fuzz-open", false) //nolint:dogsled
	})
}

// FuzzValidateHubMsgs fuzzes the validation of decoded (and already
// signature-verified) announcement and status message payloads.
func FuzzValidateHubMsgs(f *testing.F) {
	a := &Announcement{
		ID:         "Zwu5LtU3DLjoV1KNg5Vzwns3eWStB1hjUQuCQNP5bWVQmA",
		Timestamp:  time.Now().Unix(),
		Name:       "fuzz",
		Group:      "group",
		Hosters:    []string{"hoster"},
		IPv4:       []byte{1, 1, 1, 1},
		Transports: []string{"tcp://example.com:17", "http://example.com:80/spn"},
		Entry:      []string{"+ *"},
		Exit:       []string{"- * TCP/25", "- US"},
		Flags:      []string{"flag"},
	}
	data, err := dsd.Dump(a, dsd.JSON)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	s := &Status{
		Timestamp: time.Now().Unix(),
		Version:   "1.0.0",
		Keys:      map[string]*Key{"key": {Scheme: "ECDH-X25519", Key: []byte{1, 2, 3}, Expires: 1}},
		Lanes:     []*Lane{{ID: "lane", Capacity: 1, Latency: 1}},
		Load:      10,
		Flags:     []string{FlagNetError},
	}
	data, err = dsd.Dump(s, dsd.JSON)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)

	f.Fuzz(func(t *testing.T, data []byte) {
		announcement := &Announcement{}
		_, err := dsd.Load(data, announcement)
		if err == nil {
			h := &Hub{ID: announcement.ID, Info: a}
			for _, scope := range []Scope{ScopePublic, ScopeLocal, ScopeTest} {
				_ = h.validateAnnouncement(announcement, scope)
			}
			h.Info = nil
			_ = h.validateAnnouncement(announcement, ScopePublic)
			_ = h.getName()
		}

		status := &Status{}
		_, err = dsd.Load(data, status)
		if err == nil {
			h := &Hub{ID: "Zwu5LtU3DLjoV1KNg5Vzwns3eWStB1hjUQuCQNP5bWVQmA"}
			err = h.validateStatus(status)
			if err == nil {
				h.Status = status
				_ = h.SelectSignet()
				_ = h.GetLaneTo("lane")
				_ = h.HasFlag(FlagOffline)
				_ = h.Obsolete()
			}
		}
	})
}
