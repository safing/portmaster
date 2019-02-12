package firewall

// var (
// 	TunnelNet4   *net.IPNet
// 	TunnelNet6   *net.IPNet
// 	TunnelEntry4 = net.IPv4(127, 0, 0, 17)
// 	TunnelEntry6 = net.ParseIP("fd17::17")
//
// 	ipToDomainMap     = make(map[string]*TunnelInfo)
// 	ipToDomainMapLock sync.RWMutex
// )
//
// func init() {
// 	var err error
// 	_, TunnelNet4, err = net.ParseCIDR("127.17.0.0/16")
// 	if err != nil {
// 		log.Fatalf("portmaster: could not parse 127.17.0.0/16: %s", err)
// 	}
// 	_, TunnelNet6, err = net.ParseCIDR("fd17::/64")
// 	if err != nil {
// 		log.Fatalf("portmaster: could not parse fd17::/64: %s", err)
// 	}
//
// 	go tunnelInfoCleaner()
// }
//
// type TunnelInfo struct {
// 	IP      net.IP
// 	Domain  string
// 	RRCache *intel.RRCache
// 	Expires int64
// }
//
// func (ti *TunnelInfo) ExportTunnelIP() *intel.RRCache {
// 	return &intel.RRCache{
// 		Answer: []dns.RR{
// 			&dns.A{
// 				Hdr: dns.RR_Header{
// 					Name:     ti.Domain,
// 					Rrtype:   1,
// 					Class:    1,
// 					Ttl:      17,
// 					Rdlength: 8,
// 				},
// 				A: ti.IP,
// 			},
// 		},
// 	}
// }
//
// func AssignTunnelIP(domain string) (*TunnelInfo, error) {
// 	ipToDomainMapLock.Lock()
// 	defer ipToDomainMapLock.Unlock()
//
// 	for i := 0; i < 100; i++ {
// 		// get random IP
// 		r, err := random.Bytes(2)
// 		if err != nil {
// 			return nil, err
// 		}
// 		randomIP := net.IPv4(127, 17, r[0], r[1])
//
// 		// clean after every 20 tries
// 		if i > 0 && i%20 == 0 {
// 			cleanExpiredTunnelInfos()
// 		}
//
// 		// if it does not exist yet, set and return
// 		_, ok := ipToDomainMap[randomIP.String()]
// 		if !ok {
// 			tunnelInfo := &TunnelInfo{
// 				IP:      randomIP,
// 				Domain:  domain,
// 				Expires: time.Now().Add(5 * time.Minute).Unix(),
// 			}
// 			ipToDomainMap[randomIP.String()] = tunnelInfo
// 			return tunnelInfo, nil
// 		}
// 	}
//
// 	return nil, errors.New("could not find available tunnel IP, please retry later")
// }
//
// func GetTunnelInfo(tunnelIP net.IP) (tunnelInfo *TunnelInfo) {
// 	ipToDomainMapLock.RLock()
// 	defer ipToDomainMapLock.RUnlock()
// 	var ok bool
// 	tunnelInfo, ok = ipToDomainMap[tunnelIP.String()]
// 	if ok && tunnelInfo.Expires >= time.Now().Unix() {
// 		return tunnelInfo
// 	}
// 	return nil
// }
//
// func tunnelInfoCleaner() {
// 	for {
// 		time.Sleep(5 * time.Minute)
// 		ipToDomainMapLock.Lock()
// 		cleanExpiredTunnelInfos()
// 		ipToDomainMapLock.Unlock()
// 	}
// }
//
// func cleanExpiredTunnelInfos() {
// 	now := time.Now().Unix()
// 	for domain, tunnelInfo := range ipToDomainMap {
// 		if tunnelInfo.Expires < now {
// 			delete(ipToDomainMap, domain)
// 		}
// 	}
// }
