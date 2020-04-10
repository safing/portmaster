// +build !linux

package netenv

func getNameserversFromDbus() ([]Nameserver, error) {
	var nameservers []Nameserver
	return nameservers, nil
}

func getConnectivityStateFromDbus() (OnlineStatus, error) {
	return StatusUnknown, nil
}
