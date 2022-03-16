module github.com/safing/portmaster

go 1.15

require (
	github.com/agext/levenshtein v1.2.3
	github.com/cookieo9/resources-go v0.0.0-20150225115733-d27c04069d0d
	github.com/coreos/go-iptables v0.6.0
	github.com/florianl/go-nfqueue v1.3.0
	github.com/godbus/dbus/v5 v5.1.0
	github.com/google/gopacket v1.1.19
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-version v1.4.0
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/mdlayher/socket v0.2.2 // indirect
	github.com/miekg/dns v1.1.46
	github.com/oschwald/maxminddb-golang v1.8.0
	github.com/safing/portbase v0.14.0
	github.com/safing/spn v0.4.3
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cobra v1.3.0
	github.com/stretchr/testify v1.7.0
	github.com/tannerryan/ring v1.1.2
	github.com/tevino/abool v1.2.0
	github.com/umahmood/haversine v0.0.0-20151105152445-808ab04add26
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20220227234510-4e6760a101f9
	zombiezen.com/go/sqlite v0.9.2
)

replace github.com/safing/spn => ../spn

replace github.com/safing/portbase => ../portbase
