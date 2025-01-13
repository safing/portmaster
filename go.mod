module github.com/safing/portmaster

go 1.22.0

// TODO: Remove when https://github.com/tc-hib/winres/pull/4 is released.
replace github.com/tc-hib/winres => github.com/dhaavi/winres v0.2.2

require (
	fyne.io/systray v1.11.0
	github.com/VictoriaMetrics/metrics v1.35.1
	github.com/Xuanwo/go-locale v1.1.2
	github.com/aead/serpent v0.0.0-20160714141033-fba169763ea6
	github.com/agext/levenshtein v1.2.3
	github.com/armon/go-radix v1.0.0
	github.com/awalterschulze/gographviz v2.0.3+incompatible
	github.com/bluele/gcache v0.0.2
	github.com/brianvoe/gofakeit v3.18.0+incompatible
	github.com/cilium/ebpf v0.17.1
	github.com/coreos/go-iptables v0.8.0
	github.com/davecgh/go-spew v1.1.1
	github.com/dgraph-io/badger v1.6.2
	github.com/dhaavi/go-notify v0.0.0-20190209221809-c404b1f22435
	github.com/florianl/go-conntrack v0.4.0
	github.com/florianl/go-nfqueue v1.3.2
	github.com/fogleman/gg v1.3.0
	github.com/ghodss/yaml v1.0.0
	github.com/godbus/dbus/v5 v5.1.0
	github.com/gofrs/uuid v4.4.0+incompatible
	github.com/google/gopacket v1.1.19
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-version v1.7.0
	github.com/hectane/go-acl v0.0.0-20230122075934-ca0b05cb1adb
	github.com/jackc/puddle/v2 v2.2.2
	github.com/lmittmann/tint v1.0.6
	github.com/maruel/panicparse/v2 v2.4.0
	github.com/mat/besticon v3.12.0+incompatible
	github.com/mattn/go-colorable v0.1.14
	github.com/mattn/go-isatty v0.0.20
	github.com/miekg/dns v1.1.62
	github.com/mitchellh/copystructure v1.2.0
	github.com/mitchellh/go-server-timing v1.0.1
	github.com/mr-tron/base58 v1.2.0
	github.com/oschwald/maxminddb-golang v1.13.1
	github.com/r3labs/diff/v3 v3.0.1
	github.com/rot256/pblind v0.0.0-20240730113005-f3275049ead5
	github.com/safing/jess v0.3.5
	github.com/safing/structures v1.1.0
	github.com/seehuhn/fortuna v1.0.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cobra v1.8.1
	github.com/spkg/zipfs v0.7.1
	github.com/stretchr/testify v1.9.0
	github.com/tannerryan/ring v1.1.2
	github.com/tc-hib/winres v0.3.1
	github.com/tevino/abool v1.2.0
	github.com/tidwall/gjson v1.18.0
	github.com/tidwall/sjson v1.2.5
	github.com/umahmood/haversine v0.0.0-20151105152445-808ab04add26
	github.com/varlink/go v0.4.0
	github.com/vincent-petithory/dataurl v1.0.0
	go.etcd.io/bbolt v1.3.11
	golang.org/x/exp v0.0.0-20250106191152-7588d65b2ba8
	golang.org/x/image v0.23.0
	golang.org/x/net v0.34.0
	golang.org/x/sync v0.10.0
	golang.org/x/sys v0.29.0
	gopkg.in/yaml.v3 v3.0.1
	zombiezen.com/go/sqlite v1.4.0
)

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/aead/ecdh v0.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/dgraph-io/ristretto v0.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus v4.1.0+incompatible // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/golang/gddo v0.0.0-20210115222349-20d68f94ee1f // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/mdlayher/netlink v1.7.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/seehuhn/sha256d v1.0.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.9.0 // indirect
	github.com/valyala/fastrand v1.1.0 // indirect
	github.com/valyala/histogram v1.2.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zalando/go-keyring v0.2.6 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/mod v0.22.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/tools v0.29.0 // indirect
	google.golang.org/protobuf v1.36.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	modernc.org/libc v1.61.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.8.1 // indirect
	modernc.org/sqlite v1.34.4 // indirect
)
