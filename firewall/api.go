package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
)

var (
	dataRoot *utils.DirStructure

	apiPortSet bool
	apiPort    uint16
)

func prepAPIAuth() error {
	dataRoot = dataroot.Root()
	return api.SetAuthenticator(apiAuthenticator)
}

func startAPIAuth() {
	var err error
	_, apiPort, err = parseHostPort(apiListenAddress())
	if err != nil {
		log.Warningf("filter: failed to parse API address for improved api auth mechanism: %s", err)
		return
	}
	apiPortSet = true
	log.Tracef("filter: api port set to %d", apiPort)
}

func apiAuthenticator(s *http.Server, r *http.Request) (grantAccess bool, err error) {
	if devMode() {
		return true, nil
	}

	// get local IP/Port
	localIP, localPort, err := parseHostPort(s.Addr)
	if err != nil {
		return false, fmt.Errorf("failed to get local IP/Port: %s", err)
	}

	// get remote IP/Port
	remoteIP, remotePort, err := parseHostPort(r.RemoteAddr)
	if err != nil {
		return false, fmt.Errorf("failed to get remote IP/Port: %s", err)
	}

	var procsChecked []string

	// get process
	proc, _, err := process.GetProcessByEndpoints(
		r.Context(),
		packet.IPv4,
		packet.TCP,
		// switch reverse/local to get remote process
		remoteIP,
		remotePort,
		localIP,
		localPort,
		false,
	)
	if err != nil {
		return false, fmt.Errorf("failed to get process: %s", err)
	}

	// go up up to two levels, if we don't match
	for i := 0; i < 3; i++ {
		// check if the requesting process is in database root / updates dir
		if strings.HasPrefix(proc.Path, dataRoot.Path) {
			return true, nil
		}
		// add checked process to list
		procsChecked = append(procsChecked, proc.Path)

		if i < 2 {
			// get parent process
			proc, err = process.GetOrFindProcess(context.Background(), proc.ParentPid)
			if err != nil {
				return false, fmt.Errorf("failed to get process: %s", err)
			}
		}
	}

	log.Debugf("filter: denying api access to %s - also checked %s (trusted root is %s)", procsChecked[0], strings.Join(procsChecked[1:], " "), dataRoot.Path)
	return false, nil
}

func parseHostPort(address string) (net.IP, uint16, error) {
	ipString, portString, err := net.SplitHostPort(address)
	if err != nil {
		return nil, 0, err
	}

	ip := net.ParseIP(ipString)
	if ip == nil {
		return nil, 0, errors.New("invalid IP address")
	}

	port, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, 0, err
	}

	return ip, uint16(port), nil
}
