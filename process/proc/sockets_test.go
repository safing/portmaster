// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package proc

import (
	"net"
	"testing"
)

func TestSockets(t *testing.T) {

	updateListeners(TCP4)
	updateListeners(UDP4)
	updateListeners(TCP6)
	updateListeners(UDP6)
	t.Logf("addressListeningTCP4: %v", addressListeningTCP4)
	t.Logf("globalListeningTCP4: %v", globalListeningTCP4)
	t.Logf("addressListeningUDP4: %v", addressListeningUDP4)
	t.Logf("globalListeningUDP4: %v", globalListeningUDP4)
	t.Logf("addressListeningTCP6: %v", addressListeningTCP6)
	t.Logf("globalListeningTCP6: %v", globalListeningTCP6)
	t.Logf("addressListeningUDP6: %v", addressListeningUDP6)
	t.Logf("globalListeningUDP6: %v", globalListeningUDP6)

	getListeningSocket(&net.IPv4zero, 53, TCP4)
	getListeningSocket(&net.IPv4zero, 53, UDP4)
	getListeningSocket(&net.IPv6zero, 53, TCP6)
	getListeningSocket(&net.IPv6zero, 53, UDP6)

	// spotify: 192.168.0.102:5353     192.121.140.65:80
	localIP := net.IPv4(192, 168, 127, 10)
	uid, inode, ok := getConnectionSocket(&localIP, 46634, TCP4)
	t.Logf("getConnectionSocket: %d %d %v", uid, inode, ok)

	activeConnectionIDs := GetActiveConnectionIDs()
	for _, connID := range activeConnectionIDs {
		t.Logf("active: %s", connID)
	}

}
