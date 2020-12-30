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

const (
	deniedMsgUnidentified = `%wFailed to identify the requesting process.
You can enable the Development Mode to disable API authentication for development purposes.

If you are seeing this message in the Portmaster App, please restart the app or right-click and select "Reload".
In the future, this issue will be remediated automatically.`

	deniedMsgSystem = `%wSystem access to the Portmaster API is not permitted.
You can enable the Development Mode to disable API authentication for development purposes.`

	deniedMsgUnauthorized = `%wThe requesting process is not authorized to access the Portmaster API.
Checked process paths:
%s

The authorized root path is %s.
You can enable the Development Mode to disable API authentication for development purposes.`
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

func apiAuthenticator(ctx context.Context, s *http.Server, r *http.Request) (token *api.AuthToken, err error) {
	if devMode() {
		return &api.AuthToken{
			Read:  api.PermitSelf,
			Write: api.PermitSelf,
		}, nil
	}

	// get local IP/Port
	localIP, localPort, err := parseHostPort(s.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get local IP/Port: %s", err)
	}

	// get remote IP/Port
	remoteIP, remotePort, err := parseHostPort(r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote IP/Port: %s", err)
	}

	log.Tracer(r.Context()).Tracef("filter: authenticating API request from %s", r.RemoteAddr)

	// It is very important that this works, retry extensively (every 250ms for 5s)
	var retry bool
	for tries := 0; tries < 20; tries++ {
		retry, err = authenticateAPIRequest(
			ctx,
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
		if !retry {
			break
		}

		// wait a little
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}

	return &api.AuthToken{
		Read:  api.PermitSelf,
		Write: api.PermitSelf,
	}, nil
}

func authenticateAPIRequest(ctx context.Context, pktInfo *packet.Info) (retry bool, err error) {
	var procsChecked []string

	// get process
	proc, _, err := process.GetProcessByConnection(ctx, pktInfo)
	if err != nil {
		return true, fmt.Errorf("failed to get process: %s", err)
	}
	originalPid := proc.Pid
	var previousPid int

	// go up up to two levels, if we don't match
	for i := 0; i < 5; i++ {
		// check for eligible PID
		switch proc.Pid {
		case process.UnidentifiedProcessID, process.SystemProcessID:
			break
		default: // normal process
			// check if the requesting process is in database root / updates dir
			if strings.HasPrefix(proc.Path, dataRoot.Path) {
				return false, nil
			}
		}

		// add checked process to list
		procsChecked = append(procsChecked, proc.Path)

		if i < 4 {
			// save previous PID
			previousPid = proc.Pid

			// get parent process
			proc, err = process.GetOrFindProcess(ctx, proc.ParentPid)
			if err != nil {
				return true, fmt.Errorf("failed to get process: %s", err)
			}

			// abort if we are looping
			if proc.Pid == previousPid {
				// this also catches -1 pid loops
				break
			}
		}
	}

	switch originalPid {
	case process.UnidentifiedProcessID:
		log.Tracer(ctx).Warningf("filter: denying api access: failed to identify process")
		return true, fmt.Errorf(deniedMsgUnidentified, api.ErrAPIAccessDeniedMessage) //nolint:stylecheck // message for user

	case process.SystemProcessID:
		log.Tracer(ctx).Warningf("filter: denying api access: request by system")
		return false, fmt.Errorf(deniedMsgSystem, api.ErrAPIAccessDeniedMessage) //nolint:stylecheck // message for user

	default: // normal process
		log.Tracer(ctx).Warningf("filter: denying api access to %s - also checked %s (trusted root is %s)", procsChecked[0], strings.Join(procsChecked[1:], " "), dataRoot.Path)
		return false, fmt.Errorf( //nolint:stylecheck // message for user
			deniedMsgUnauthorized,
			api.ErrAPIAccessDeniedMessage,
			strings.Join(procsChecked, "\n"),
			dataRoot.Path,
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
