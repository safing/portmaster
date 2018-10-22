// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/Safing/portbase/log"
)

const (
	DNSClassMulticast = dns.ClassINET | 1<<15
)

var (
	multicast4Conn *net.UDPConn
	multicast6Conn *net.UDPConn
	unicast4Conn   *net.UDPConn
	unicast6Conn   *net.UDPConn

	questions     = make(map[uint16]savedQuestion)
	questionsLock sync.Mutex
)

type savedQuestion struct {
	question dns.Question
	expires  int64
}

func init() {
	go listenToMDNS()
}

func indexOfRR(entry *dns.RR_Header, list *[]dns.RR) int {
	for k, v := range *list {
		if entry.Name == v.Header().Name && entry.Rrtype == v.Header().Rrtype {
			return k
		}
	}
	return -1
}

func listenToMDNS() {
	var err error
	messages := make(chan *dns.Msg)

	multicast4Conn, err = net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
	if err != nil {
		// TODO: retry after some time
		log.Warningf("intel(mdns): failed to create udp4 listen multicast socket: %s", err)
	} else {
		go listenForDNSPackets(multicast4Conn, messages)
	}

	multicast6Conn, err = net.ListenMulticastUDP("udp6", nil, &net.UDPAddr{IP: net.IP([]byte{0xff, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfb}), Port: 5353})
	if err != nil {
		// TODO: retry after some time
		log.Warningf("intel(mdns): failed to create udp6 listen multicast socket: %s", err)
	} else {
		go listenForDNSPackets(multicast6Conn, messages)
	}

	unicast4Conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		// TODO: retry after some time
		log.Warningf("intel(mdns): failed to create udp4 listen socket: %s", err)
	} else {
		go listenForDNSPackets(unicast4Conn, messages)
	}

	unicast6Conn, err = net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err != nil {
		// TODO: retry after some time
		log.Warningf("intel(mdns): failed to create udp6 listen socket: %s", err)
	} else {
		go listenForDNSPackets(unicast6Conn, messages)
	}

	for {
		select {
		case message := <-messages:
			// log.Tracef("intel: got net mdns message: %s", message)

			var question *dns.Question
			var saveFullRequest bool
			scavengedRecords := make(map[string]dns.RR)
			var rrCache *RRCache

			// save every received response
			// if previous save was less than 2 seconds ago, add to response, else replace
			// pick out A and AAAA records and save seperately

			// continue if not response
			if !message.Response {
				// log.Tracef("intel: mdns message has no response, ignoring")
				continue
			}

			// continue if rcode is not success
			if message.Rcode != dns.RcodeSuccess {
				// log.Tracef("intel: mdns message has error, ignoring")
				continue
			}

			// continue if answer section is empty
			if len(message.Answer) == 0 {
				// log.Tracef("intel: mdns message has no answers, ignoring")
				continue
			}

			// get question, some servers do not reply with question
			if len(message.Question) == 0 {
				questionsLock.Lock()
				savedQ, ok := questions[message.MsgHdr.Id]
				questionsLock.Unlock()
				if ok {
					question = &savedQ.question
				}
			} else {
				question = &message.Question[0]
			}

			if question != nil {
				// continue if class is not INTERNET
				if question.Qclass != dns.ClassINET && question.Qclass != DNSClassMulticast {
					// log.Tracef("intel: mdns question is not of class INET, ignoring")
					continue
				}
				saveFullRequest = true
			}

			// get entry from database
			if saveFullRequest {
				rrCache, err = GetRRCache(question.Name, dns.Type(question.Qtype))
				if err != nil || rrCache.updated < time.Now().Add(-2*time.Second).Unix() || rrCache.TTL < time.Now().Unix() {
					rrCache = &RRCache{
						Domain:   question.Name,
						Question: dns.Type(question.Qtype),
					}
				}
			}

			for _, entry := range message.Answer {
				if strings.HasSuffix(entry.Header().Name, ".local.") || domainInScopes(entry.Header().Name, localReverseScopes) {
					if saveFullRequest {
						k := indexOfRR(entry.Header(), &rrCache.Answer)
						if k == -1 {
							rrCache.Answer = append(rrCache.Answer, entry)
						} else {
							rrCache.Answer[k] = entry
						}
					}
					switch entry.(type) {
					case *dns.A:
						scavengedRecords[fmt.Sprintf("%sA", entry.Header().Name)] = entry
					case *dns.AAAA:
						scavengedRecords[fmt.Sprintf("%sAAAA", entry.Header().Name)] = entry
					case *dns.PTR:
						if !strings.HasPrefix(entry.Header().Name, "_") {
							scavengedRecords[fmt.Sprintf("%sPTR", entry.Header().Name)] = entry
						}
					}
				}
			}
			for _, entry := range message.Ns {
				if strings.HasSuffix(entry.Header().Name, ".local.") || domainInScopes(entry.Header().Name, localReverseScopes) {
					if saveFullRequest {
						k := indexOfRR(entry.Header(), &rrCache.Ns)
						if k == -1 {
							rrCache.Ns = append(rrCache.Ns, entry)
						} else {
							rrCache.Ns[k] = entry
						}
					}
					switch entry.(type) {
					case *dns.A:
						scavengedRecords[fmt.Sprintf("%s_A", entry.Header().Name)] = entry
					case *dns.AAAA:
						scavengedRecords[fmt.Sprintf("%s_AAAA", entry.Header().Name)] = entry
					case *dns.PTR:
						if !strings.HasPrefix(entry.Header().Name, "_") {
							scavengedRecords[fmt.Sprintf("%s_PTR", entry.Header().Name)] = entry
						}
					}
				}
			}
			for _, entry := range message.Extra {
				if strings.HasSuffix(entry.Header().Name, ".local.") || domainInScopes(entry.Header().Name, localReverseScopes) {
					if saveFullRequest {
						k := indexOfRR(entry.Header(), &rrCache.Extra)
						if k == -1 {
							rrCache.Extra = append(rrCache.Extra, entry)
						} else {
							rrCache.Extra[k] = entry
						}
					}
					switch entry.(type) {
					case *dns.A:
						scavengedRecords[fmt.Sprintf("%sA", entry.Header().Name)] = entry
					case *dns.AAAA:
						scavengedRecords[fmt.Sprintf("%sAAAA", entry.Header().Name)] = entry
					case *dns.PTR:
						if !strings.HasPrefix(entry.Header().Name, "_") {
							scavengedRecords[fmt.Sprintf("%sPTR", entry.Header().Name)] = entry
						}
					}
				}
			}

			var questionID string
			if saveFullRequest {
				rrCache.Clean(60)
				rrCache.Save()
				questionID = fmt.Sprintf("%s%s", question.Name, dns.Type(question.Qtype).String())
			}

			for k, v := range scavengedRecords {
				if saveFullRequest && k == questionID {
					continue
				}
				rrCache = &RRCache{
					Domain:   v.Header().Name,
					Question: dns.Type(v.Header().Class),
					Answer:   []dns.RR{v},
				}
				rrCache.Clean(60)
				rrCache.Save()
				// log.Tracef("intel: mdns scavenged %s", k)
			}

		}

		cleanSavedQuestions()

	}

}

func listenForDNSPackets(conn *net.UDPConn, messages chan *dns.Msg) {
	buf := make([]byte, 65536)
	for {
		// log.Tracef("debug: listening...")
		n, err := conn.Read(buf)
		// n, _, err := conn.ReadFrom(buf)
		// n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// log.Tracef("intel: failed to read packet: %s", err)
			continue
		}
		// log.Tracef("debug: read something...")
		message := new(dns.Msg)
		if err = message.Unpack(buf[:n]); err != nil {
			// log.Tracef("intel: failed to unpack message: %s", err)
			continue
		}
		// log.Tracef("debug: parsed message...")
		messages <- message
	}
}

func queryMulticastDNS(fqdn string, qtype dns.Type) (*RRCache, error) {
	q := new(dns.Msg)
	q.SetQuestion(fqdn, uint16(qtype))
	// request unicast response
	// q.Question[0].Qclass |= 1 << 15
	q.RecursionDesired = false

	saveQuestion(q)

	questionsLock.Lock()
	defer questionsLock.Unlock()
	questions[q.MsgHdr.Id] = savedQuestion{
		question: q.Question[0],
		expires:  time.Now().Add(10 * time.Second).Unix(),
	}

	buf, err := q.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack query: %s", err)
	}
	if unicast4Conn == nil && unicast6Conn == nil {
		return nil, errors.New("unicast mdns connections not initialized")
	}
	if unicast4Conn != nil && uint16(qtype) != dns.TypeAAAA {
		unicast4Conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err = unicast4Conn.WriteToUDP(buf, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
		if err != nil {
			return nil, fmt.Errorf("failed to send query: %s", err)
		}
	}
	if unicast6Conn != nil && uint16(qtype) != dns.TypeA {
		unicast6Conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err = unicast6Conn.WriteToUDP(buf, &net.UDPAddr{IP: net.IP([]byte{0xff, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfb}), Port: 5353})
		if err != nil {
			return nil, fmt.Errorf("failed to send query: %s", err)
		}
	}

	time.Sleep(1 * time.Second)

	rrCache, err := GetRRCache(fqdn, qtype)
	if err == nil {
		return rrCache, nil
	}

	return nil, nil
}

func saveQuestion(q *dns.Msg) {
	questionsLock.Lock()
	defer questionsLock.Unlock()
	// log.Tracef("intel: saving mdns question id=%d, name=%s", q.MsgHdr.Id, q.Question[0].Name)
	questions[q.MsgHdr.Id] = savedQuestion{
		question: q.Question[0],
		expires:  time.Now().Add(10 * time.Second).Unix(),
	}
}

func cleanSavedQuestions() {
	questionsLock.Lock()
	defer questionsLock.Unlock()
	now := time.Now().Unix()
	for k, v := range questions {
		if v.expires < now {
			delete(questions, k)
		}
	}
}
