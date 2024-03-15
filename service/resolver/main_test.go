package resolver

import (
	"testing"

	"github.com/safing/portmaster/service/core/pmtesting"
)

var domainFeed = make(chan string)

func TestMain(m *testing.M) {
	pmtesting.TestMain(m, module)
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
