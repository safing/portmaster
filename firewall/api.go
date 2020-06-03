package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

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

func apiAuthenticator(s *http.Server, r *http.Request) (err error) {
	if devMode() {
		return nil
	}

	// get local IP/Port
	localIP, localPort, err := parseHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("failed to get local IP/Port: %s", err)
	}

	// get remote IP/Port
	remoteIP, remotePort, err := parseHostPort(r.RemoteAddr)
	if err != nil {
		return fmt.Errorf("failed to get remote IP/Port: %s", err)
	}

	// It is very important that this works, retry extensively (every 250ms for 5s)
	for tries := 0; tries < 20; tries++ {
		err = authenticateAPIRequest(
			r.Context(),
			&packet.Info{
				Inbound:  false, // outbound as we are looking for the process of the source address
				Version:  packet.IPv4,
				Protocol: packet.TCP,
				Src:      remoteIP,   // source as in the process we are looking for
				SrcPort:  remotePort, // source as in the process we are looking for
				Dst:      localIP,
				DstPort:  localPort,
			},
		)
		if err != nil {
			return nil
		}

		// wait a little
		time.Sleep(250 * time.Millisecond)
	}

	return err
}

func authenticateAPIRequest(ctx context.Context, pktInfo *packet.Info) error {
	var procsChecked []string

	// get process
	proc, _, err := process.GetProcessByConnection(ctx, pktInfo)
	if err != nil {
		return fmt.Errorf("failed to get process: %s", err)
	}
	originalPid := proc.Pid

	// go up up to two levels, if we don't match
	for i := 0; i < 5; i++ {
		// check for eligible PID
		switch proc.Pid {
		case process.UnidentifiedProcessID, process.SystemProcessID:
			break
		default: // normal process
			// check if the requesting process is in database root / updates dir
			if strings.HasPrefix(proc.Path, dataRoot.Path) {
				return nil
			}
		}

		// add checked process to list
		procsChecked = append(procsChecked, proc.Path)

		if i < 2 {
			// get parent process
			proc, err = process.GetOrFindProcess(context.Background(), proc.ParentPid)
			if err != nil {
				return fmt.Errorf("failed to get process: %s", err)
			}
		}
	}

	switch originalPid {
	case process.UnidentifiedProcessID:
		log.Warningf("filter: denying api access: failed to identify process")
		return fmt.Errorf("%wFailed to identify the requesting process. You can enable the Development Mode to disable API authentication for development purposes.", api.ErrAPIAccessDeniedMessage)

	case process.SystemProcessID:
		log.Warningf("filter: denying api access: request by system")
		return fmt.Errorf("%wSystem access to the Portmaster API is not permitted. You can enable the Development Mode to disable API authentication for development purposes.", api.ErrAPIAccessDeniedMessage)

	default: //
		log.Warningf("filter: denying api access to %s - also checked %s (trusted root is %s)", procsChecked[0], strings.Join(procsChecked[1:], " "), dataRoot.Path)
		return fmt.Errorf(
			`%wThe requesting process is not authorized to access the Portmaster API.
Checked process paths:
%s

The authorithed root path is %s.
You can enable the Development Mode to disable API authentication for development purposes.`,
			api.ErrAPIAccessDeniedMessage,
			dataRoot.Path,
			strings.Join(procsChecked, "\n"),
		)
	}
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
