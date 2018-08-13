// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package interception

import "github.com/Safing/safing-core/network/packet"

var Packets chan packet.Packet

func init() {
	Packets = make(chan packet.Packet, 1000)
}
