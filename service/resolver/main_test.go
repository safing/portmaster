package resolver

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/updates"
)

var domainFeed = make(chan string)

type testInstance struct {
	db      *dbmodule.DBModule
	base    *base.Base
	api     *api.API
	config  *config.Config
	updates *updates.Updates
	netenv  *netenv.NetEnv
}

// var _ instance = &testInstance{}

func (stub *testInstance) Updates() *updates.Updates {
	return stub.updates
}

func (stub *testInstance) API() *api.API {
	return stub.api
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) NetEnv() *netenv.NetEnv {
	return stub.netenv
}

func (stub *testInstance) Notifications() *notifications.Notifications {
	return nil
}

func (stub *testInstance) Ready() bool {
	return true
}

func (stub *testInstance) Restart() {}

func (stub *testInstance) Shutdown() {}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func (stub *testInstance) GetEventSPNConnected() *mgr.EventMgr[struct{}] {
	return mgr.NewEventMgr[struct{}]("spn connect", nil)
}

func runTest(m *testing.M) error {
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")
	ds, err := config.InitializeUnitTestDataroot("test-resolver")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	stub := &testInstance{}
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.base, err = base.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create base: %w", err)
	}
	stub.api, err = api.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	stub.netenv, err = netenv.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create netenv: %w", err)
	}
	stub.updates, err = updates.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
	}
	module, err := New(stub)
	if err != nil {
		return fmt.Errorf("failed to create module: %w", err)
	}

	err = stub.db.Start()
	if err != nil {
		return fmt.Errorf("Failed to start database: %w", err)
	}
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("Failed to start config: %w", err)
	}
	err = stub.base.Start()
	if err != nil {
		return fmt.Errorf("Failed to start base: %w", err)
	}
	err = stub.api.Start()
	if err != nil {
		return fmt.Errorf("Failed to start api: %w", err)
	}
	err = stub.updates.Start()
	if err != nil {
		return fmt.Errorf("Failed to start updates: %w", err)
	}
	err = stub.netenv.Start()
	if err != nil {
		return fmt.Errorf("Failed to start netenv: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("Failed to start module: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}

func init() {
	go feedDomains()
}

func feedDomains() {
	for {
		for _, domain := range testDomains {
			domainFeed <- domain
		}
	}
}

// Data

var testDomains = []string{
	"facebook.com.",
	"google.com.",
	"youtube.com.",
	"twitter.com.",
	"instagram.com.",
	"linkedin.com.",
	"microsoft.com.",
	"apple.com.",
	"wikipedia.org.",
	"plus.google.com.",
	"en.wikipedia.org.",
	"googletagmanager.com.",
	"youtu.be.",
	"adobe.com.",
	"vimeo.com.",
	"pinterest.com.",
	"itunes.apple.com.",
	"play.google.com.",
	"maps.google.com.",
	"goo.gl.",
	"wordpress.com.",
	"blogspot.com.",
	"bit.ly.",
	"github.com.",
	"player.vimeo.com.",
	"amazon.com.",
	"wordpress.org.",
	"docs.google.com.",
	"yahoo.com.",
	"mozilla.org.",
	"tumblr.com.",
	"godaddy.com.",
	"flickr.com.",
	"parked-content.godaddy.com.",
	"drive.google.com.",
	"support.google.com.",
	"apache.org.",
	"gravatar.com.",
	"europa.eu.",
	"qq.com.",
	"w3.org.",
	"nytimes.com.",
	"reddit.com.",
	"macromedia.com.",
	"get.adobe.com.",
	"soundcloud.com.",
	"sourceforge.net.",
	"sites.google.com.",
	"nih.gov.",
	"amazonaws.com.",
	"t.co.",
	"support.microsoft.com.",
	"forbes.com.",
	"theguardian.com.",
	"cnn.com.",
	"github.io.",
	"bbc.co.uk.",
	"dropbox.com.",
	"whatsapp.com.",
	"medium.com.",
	"creativecommons.org.",
	"www.ncbi.nlm.nih.gov.",
	"httpd.apache.org.",
	"archive.org.",
	"ec.europa.eu.",
	"php.net.",
	"apps.apple.com.",
	"weebly.com.",
	"support.apple.com.",
	"weibo.com.",
	"wixsite.com.",
	"issuu.com.",
	"who.int.",
	"paypal.com.",
	"m.facebook.com.",
	"oracle.com.",
	"msn.com.",
	"gnu.org.",
	"tinyurl.com.",
	"reuters.com.",
	"l.facebook.com.",
	"cloudflare.com.",
	"wsj.com.",
	"washingtonpost.com.",
	"domainmarket.com.",
	"imdb.com.",
	"bbc.com.",
	"bing.com.",
	"accounts.google.com.",
	"vk.com.",
	"api.whatsapp.com.",
	"opera.com.",
	"cdc.gov.",
	"slideshare.net.",
	"wpa.qq.com.",
	"harvard.edu.",
	"mit.edu.",
	"code.google.com.",
	"wikimedia.org.",
}
