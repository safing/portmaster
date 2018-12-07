package profile

import (
	"testing"
	"time"
)

func TestPorts(t *testing.T) {
	var ports Ports
	ports = map[int16][]*Port{
		6: []*Port{
			&Port{ // SSH
				Permit:  true,
				Created: time.Now().Unix(),
				Start:   22,
				End:     22,
			},
		},
		-17: []*Port{
			&Port{ // HTTP
				Permit:  false,
				Created: time.Now().Unix(),
				Start:   80,
				End:     81,
			},
		},
		93: []*Port{
			&Port{ // HTTP
				Permit:  true,
				Created: time.Now().Unix(),
				Start:   93,
				End:     93,
			},
		},
	}
	if ports.String() != "TCP:[permit:22], <UDP:[deny:80-81], 93:[permit:93]" {
		t.Errorf("unexpected result: %s", ports.String())
	}

	var noPorts Ports
	noPorts = map[int16][]*Port{}
	if noPorts.String() != "None" {
		t.Errorf("unexpected result: %s", ports.String())
	}

}
