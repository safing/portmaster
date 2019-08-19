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
