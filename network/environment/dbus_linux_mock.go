// +build !linux

package environment

func getNameserversFromDbus() ([]Nameserver, error) {
	var nameservers []Nameserver
	return nameservers, nil
}

func getConnectivityStateFromDbus() (uint8, error) {
	return UNKNOWN, nil
}
