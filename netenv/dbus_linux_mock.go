// +build !linux

package netenv

func getNameserversFromDbus() ([]Nameserver, error) {
	var nameservers []Nameserver
	return nameservers, nil
}

func getConnectivityStateFromDbus() (uint8, error) {
	return StatusUnknown, nil
}
