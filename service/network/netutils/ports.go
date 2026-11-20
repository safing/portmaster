package netutils

// IsEncryptedPort returns whether connections to the given destination port
// are regarded as encrypted. This is a guess based on well-known ports of
// protocols that are always encrypted; the traffic itself is not inspected.
func IsEncryptedPort(port uint16) bool {
	switch port {
	case
		22,  // SSH
		443, // HTTPS
		465, // SMTP-SSL
		853, // DoT
		993, // IMAP-SSL
		995: // POP3-SSL
		return true
	default:
		return false
	}
}
