// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package proc

import (
	"log"
	"testing"
)

func TestProcessFinder(t *testing.T) {

	updatePids()
	log.Printf("pidsByUser: %v", pidsByUser)

	pid, _ := GetPidOfInode(1000, 112588)
	log.Printf("pid: %d", pid)

}
