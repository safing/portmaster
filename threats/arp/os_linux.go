package arp

import (
	"bufio"
	"os"
	"strings"

	"github.com/Safing/portbase/log"
)

const (
	arpTableProcFile = "/proc/net/arp"
)

func getArpTable() (table []*arpEntry, err error) {
	// open file
	arpData, err := os.Open(arpTableProcFile)
	if err != nil {
		log.Warningf("threats/arp: could not read %s: %s", arpTableProcFile, err)
		return nil, err
	}
	defer arpData.Close()

	// file scanner
	scanner := bufio.NewScanner(arpData)
	scanner.Split(bufio.ScanLines)

	// parse
	scanner.Scan() // skip first line
	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if len(line) < 6 {
			continue
		}

		table = append(table, &arpEntry{
			IP:        line[0],
			MAC:       line[3],
			Interface: line[5],
		})
	}

	return table, nil
}

func clearArpTable() error {
	return nil
}
