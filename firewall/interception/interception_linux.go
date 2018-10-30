// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package interception

import "github.com/Safing/portmaster/network/packet"

var (
	Packets = make(chan packet.Packet, 1000)
)
