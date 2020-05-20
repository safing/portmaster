package resolver

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"
)

var (
	domainFeed = make(chan string)
)

func testQuery(t *testing.T, wg *sync.WaitGroup, newCnt *uint32, brc *BasicResolverConn, q *Query) {
	dnsClient := brc.clientManager.getDNSClient()

	// create query
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	// get connection
	conn, new, err := dnsClient.getConn()
	if err != nil {
		t.Fatalf("failed to connect: %s", err) //nolint:staticcheck
	}
	if new {
		atomic.AddUint32(newCnt, 1)
	}

	// query server
	reply, ttl, err := dnsClient.client.ExchangeWithConn(dnsQuery, conn)
	if err != nil {
		t.Fatal(err) //nolint:staticcheck
	}
	if reply == nil {
		t.Fatalf("resolved %s, but reply was empty!", q.FQDN) //nolint:staticcheck
	}

	t.Logf("resolved %s [new resolver = %v] in %s", q.FQDN, new, ttl)
	dnsClient.addToPool()
	wg.Done()
}

func TestClientPooling(t *testing.T) {
	// skip if short - this test depends on the Internet and might fail randomly
	if testing.Short() {
		t.Skip()
	}

	go feedDomains()

	// create separate resolver for this test
	resolver, _, err := createResolver("dot://9.9.9.9:853?verify=dns.quad9.net&name=Quad9&blockedif=empty", "config")
	if err != nil {
		t.Fatal(err)
	}
	brc := resolver.Conn.(*BasicResolverConn)

	wg := &sync.WaitGroup{}
	var newCnt uint32
	for i := 0; i < 10; i++ {
		wg.Add(10)
		for i := 0; i < 10; i++ {
			go testQuery(t, wg, &newCnt, brc, &Query{ //nolint:staticcheck
				FQDN:  <-domainFeed,
				QType: dns.Type(dns.TypeA),
			})
		}
		wg.Wait()
		if newCnt > uint32(10+i) {
			t.Fatalf("unexpected pool size: %d (limit is %d)", newCnt, 10+i)
		}
	}
}

func feedDomains() {
	for {
		for _, domain := range poolingTestDomains {
			domainFeed <- domain
		}
	}
}

// Data

var (
	poolingTestDomains = []string{
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
)
