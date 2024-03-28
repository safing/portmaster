//+build !windows,!linux

package netenv

import "net"

func Nameservers() []Nameserver {
	return nil
}

func Gateways() []net.IP {
	return nil
}

// TODO: implement using
// ifconfig
// scutil --nwi
// scutil --proxy
// networksetup -listallnetworkservices
// networksetup -listnetworkserviceorder
// networksetup -getdnsservers "Wi-Fi"
// networksetup -getsearchdomains <networkservice>
// networksetup -getftpproxy <networkservice>
// networksetup -getwebproxy <networkservice>
// networksetup -getsecurewebproxy <networkservice>
// networksetup -getstreamingproxy <networkservice>
// networksetup -getgopherproxy <networkservice>
// networksetup -getsocksfirewallproxy <networkservice>
// route -n get default
