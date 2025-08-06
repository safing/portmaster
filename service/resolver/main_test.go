package resolver

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
)

var domainFeed = make(chan string)

type testInstance struct {
	db     *dbmodule.DBModule
	base   *base.Base
	config *config.Config
	netenv *netenv.NetEnv
}

func (stub *testInstance) GetEventSPNConnected() *mgr.EventMgr[struct{}] {
	return mgr.NewEventMgr[struct{}]("spn connect", nil)
}
func (stub *testInstance) IntelUpdates() *updates.Updater     { return nil }
func (stub *testInstance) Config() *config.Config             { return stub.config }
func (stub *testInstance) NetEnv() *netenv.NetEnv             { return stub.netenv }
func (stub *testInstance) Ready() bool                        { return true }
func (stub *testInstance) SetCmdLineOperation(f func() error) {}
func (stub *testInstance) UI() *ui.UI                         { return nil }
func (stub *testInstance) DataDir() string                    { return _dataDir }

var _dataDir string

func runTest(m *testing.M) error {
	var err error

	// Create a temporary directory for testing
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		fmt.Printf("failed to create temporary data directory: %s", err)
		os.Exit(0)
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	// Set the default API listen address
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")

	// Initialize the instance with the necessary components
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
	stub.netenv, err = netenv.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create netenv: %w", err)
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
