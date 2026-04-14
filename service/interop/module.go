package interop

import (
	"errors"
	"net"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/firewall"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/interop/ivpn"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/spn/hub"
)

// Interface for separate interoperability objects.
// Each object can implement interoperability with a specific third-party application,
// such as IVPN client, to exchange information and coordinate actions for better compatibility and user experience.
type interopModule interface {
	Start() error
	PingHandler() error
	VerdictHandler(conn *network.Connection) (network.Verdict, string, bool)
}

type instance interface {
	Config() *config.Config
	Interception() *interception.Interception
	GetHookSPNConnecting() *mgr.HookMgr[hub.Announcement]
}

// Module for interoperability with third-party applications
type Interoperability struct {
	mgr                      *mgr.Manager
	instance                 instance
	cfgDnsNameServers        config.StringArrayOption
	dnsListenAddress         string
	interopModules           []interopModule
	verdictHandlerRegistered atomic.Bool
}

// Manager returns the module manager.
func (u *Interoperability) Manager() *mgr.Manager {
	return u.mgr
}

// Start starts the module.
func (u *Interoperability) Start() error {
	return start()
}

// Stop stops the module.
func (u *Interoperability) Stop() error {
	return stop()
}

func (u *Interoperability) DnsListenAddress() string {
	return u.dnsListenAddress
}
func (u *Interoperability) DnsNameServers() []string {
	return u.cfgDnsNameServers()
}
func (u *Interoperability) EvtConfigChange() <-chan struct{} {
	return u.instance.Config().EventConfigChange.Subscribe("interoperability: config change detection", 10).Events()
}
func (u *Interoperability) Interception() *interception.Interception {
	return u.instance.Interception()
}
func (u *Interoperability) GetHookSPNConnecting() *mgr.HookMgr[hub.Announcement] {
	return u.instance.GetHookSPNConnecting()
}

func start() error {
	for _, im := range module.interopModules {
		if err := im.Start(); err != nil {
			return errors.New("failed to start interoperability module: " + err.Error())
		}
	}
	return nil
}

func stop() error {
	return nil
}

var (
	module     *Interoperability
	shimLoaded atomic.Bool
)

func New(instance instance) (*Interoperability, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	// Determine Portmaster's local DNS resolver listen address for use in IVPN client configuration.
	// Ot is ok to check it once at startup, since it can only be changed by restarting Portmaster with a different config.
	var err error
	dnsListenAddress := config.GetAsString("dns/listenAddress", "")()
	dnsListenAddress, _, err = net.SplitHostPort(dnsListenAddress)
	if err != nil || net.ParseIP(dnsListenAddress) == nil || dnsListenAddress == "" || dnsListenAddress == "0.0.0.0" {
		dnsListenAddress = "127.0.0.1"
	}

	// Create module instance.
	m := mgr.New("Interop")
	module = &Interoperability{
		mgr:               m,
		instance:          instance,
		cfgDnsNameServers: config.GetAsStringArray("dns/nameservers", []string{}),
		dnsListenAddress:  dnsListenAddress,
		interopModules:    make([]interopModule, 0, 1),
	}

	if err := module.prep(); err != nil {
		return nil, err
	}

	return module, nil
}

func (i *Interoperability) prep() error {
	i.interopModules = append(i.interopModules, ivpn.NewInteropIvpn(i))
	return i.registerAPIEndpoints()
}

// EnsureVerdictHandlerRegistered registers the interoperability module's verdict handler
// with the firewall module if not already registered.
// We do this lazily here only when we need it.
func (i *Interoperability) EnsureVerdictHandlerRegistered() {
	if i.verdictHandlerRegistered.CompareAndSwap(false, true) {
		firewall.SetExternalVerdictHandler(i.verdict_handler)
	}
}
