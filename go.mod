module github.com/safing/portmaster

go 1.15

require (
	github.com/aead/ecdh v0.2.0 // indirect
	github.com/agext/levenshtein v1.2.3
	github.com/cookieo9/resources-go v0.0.0-20150225115733-d27c04069d0d
	github.com/coreos/go-iptables v0.5.0
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/florianl/go-nfqueue v1.2.0
	github.com/go-ole/go-ole v1.2.5 // indirect
	github.com/godbus/dbus/v5 v5.0.3
	github.com/gofrs/uuid v4.0.0+incompatible // indirect
	github.com/google/gopacket v1.1.19
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/klauspost/reedsolomon v1.9.11 // indirect
	github.com/mdlayher/netlink v1.3.0 // indirect
	github.com/miekg/dns v1.1.38
	github.com/oschwald/maxminddb-golang v1.8.0
	github.com/safing/jess v0.2.1 // indirect
	github.com/safing/portbase v0.9.4
	github.com/safing/spn v0.2.4
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/shirou/gopsutil v3.21.1+incompatible
	github.com/spf13/cobra v1.0.0
	github.com/stretchr/testify v1.6.1
	github.com/tannerryan/ring v1.1.2
	github.com/templexxx/cpufeat v0.0.0-20180724012125-cef66df7f161 // indirect
	github.com/templexxx/xor v0.0.0-20191217153810-f85b25db303b // indirect
	github.com/tevino/abool v1.2.0
	github.com/tidwall/sjson v1.1.5 // indirect
	github.com/tjfoc/gmsm v1.4.0 // indirect
	github.com/umahmood/haversine v0.0.0-20151105152445-808ab04add26
	github.com/xtaci/kcp-go v5.4.20+incompatible // indirect
	github.com/xtaci/lossyconn v0.0.0-20200209145036-adba10fffc37 // indirect
	golang.org/x/net v0.0.0-20210119194325-5f4716e94777
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776 // indirect
	github.com/VictoriaMetrics/metrics v1.13.1 // indirect
	github.com/klauspost/cpuid/v2 v2.0.3 // indirect
	go.etcd.io/bbolt v1.3.5 // indirect
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
)

require (
	// The follow-up commit removes Windows support.
	// TODO: Check how we want to handle this in the future, possibly ingest
	// needed functionality into here.
	github.com/google/renameio v0.1.1-0.20200217212219-353f81969824
)
