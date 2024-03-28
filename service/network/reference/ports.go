package reference

import (
	"strconv"
	"strings"
)

var (
	portNames = map[uint16]string{
		20:  "FTP-DATA",
		21:  "FTP",
		22:  "SSH",
		23:  "TELNET",
		25:  "SMTP",
		43:  "WHOIS",
		53:  "DNS",
		67:  "DHCP_SERVER",
		68:  "DHCP_CLIENT",
		69:  "TFTP",
		80:  "HTTP",
		110: "POP3",
		123: "NTP",
		143: "IMAP",
		161: "SNMP",
		179: "BGP",
		194: "IRC",
		389: "LDAP",
		443: "HTTPS",
		445: "SMB",
		587: "SMTP_ALT",
		465: "SMTP_SSL",
		993: "IMAP_SSL",
		995: "POP3_SSL",
	}

	portNumbers = map[string]uint16{
		"FTP-DATA":    20,
		"FTP":         21,
		"SSH":         22,
		"TELNET":      23,
		"SMTP":        25,
		"WHOIS":       43,
		"DNS":         53,
		"DHCP-SERVER": 67,
		"DHCP_SERVER": 67,
		"DHCP-CLIENT": 68,
		"DHCP_CLIENT": 68,
		"TFTP":        69,
		"HTTP":        80,
		"POP3":        110,
		"NTP":         123,
		"IMAP":        143,
		"SNMP":        161,
		"BGP":         179,
		"IRC":         194,
		"LDAP":        389,
		"HTTPS":       443,
		"SMB":         445,
		"SMTP-ALT":    587,
		"SMTP_ALT":    587,
		"SMTP-SSL":    465,
		"SMTP_SSL":    465,
		"IMAP-SSL":    993,
		"IMAP_SSL":    993,
		"POP3-SSL":    995,
		"POP3_SSL":    995,
	}
)

// GetPortName returns the name of a port number.
func GetPortName(port uint16) (name string) {
	name, ok := portNames[port]
	if ok {
		return name
	}
	return strconv.Itoa(int(port))
}

// GetPortNumber returns the number of a port name.
func GetPortNumber(port string) (number uint16, ok bool) {
	number, ok = portNumbers[strings.ToUpper(port)]
	if ok {
		return number, true
	}
	return 0, false
}
