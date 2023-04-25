package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/updates"
)

const (
	deniedMsgUnidentified = `%wFailed to identify the requesting process. Reload to try again.`

	deniedMsgSystem = `%wSystem access to the Portmaster API is not permitted.
You can enable the Development Mode to disable API authentication for development purposes.`

	deniedMsgUnauthorized = `%wThe requesting process is not authorized to access the Portmaster API.
Checked process paths:
%s

The authorized root path is %s.
You can enable the Development Mode to disable API authentication for development purposes.
For production use please create an API key in the settings.`

	deniedMsgMisconfigured = `%wThe authentication system is misconfigured.`
)

var (
	dataRoot *utils.DirStructure

	apiPortSet bool
	apiIP      net.IP
	apiPort    uint16
)

func prepAPIAuth() error {
	dataRoot = dataroot.Root()
	return api.SetAuthenticator(apiAuthenticator)
}

func startAPIAuth() {
	var err error
	apiIP, apiPort, err = parseHostPort(apiListenAddress())
	if err != nil {
		log.Warningf("filter: failed to parse API address for improved api auth mechanism: %s", err)
		return
	}
	apiPortSet = true
	log.Tracef("filter: api port set to %d", apiPort)
}

func apiAuthenticator(r *http.Request, s *http.Server) (token *api.AuthToken, err error) {
	if configReady.IsSet() && devMode() {
		return &api.AuthToken{
			Read:  api.PermitSelf,
			Write: api.PermitSelf,
		}, nil
	}

	// get local IP/Port
	localIP, localPort, err := parseHostPort(s.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get local IP/Port: %w", err)
	}

	// get remote IP/Port
	remoteIP, remotePort, err := parseHostPort(r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote IP/Port: %w", err)
	}

	// Check if the request is even local.
	myIP, err := netenv.IsMyIP(remoteIP)
	if err == nil && !myIP {
		// Return to caller that the request was not handled.
		return nil, nil
	}

	log.Tracer(r.Context()).Tracef("filter: authenticating API request from %s", r.RemoteAddr)

	// It is important that this works, retry 5 times: every 500ms for 2.5s.
	var retry bool
	for tries := 0; tries < 5; tries++ {
		retry, err = authenticateAPIRequest(
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
		if !retry {
			break
		}

		// wait a little
		time.Sleep(500 * time.Millisecond)
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
	var originalPid int

	// Get authenticated path.
	authenticatedPath := updates.RootPath()
	if authenticatedPath == "" {
		return false, fmt.Errorf(deniedMsgMisconfigured, api.ErrAPIAccessDeniedMessage) //nolint:stylecheck // message for user
	}
	// Get real path.
	authenticatedPath, err = filepath.EvalSymlinks(authenticatedPath)
	if err != nil {
		return false, fmt.Errorf(deniedMsgUnidentified, api.ErrAPIAccessDeniedMessage) //nolint:stylecheck // message for user
	}
	// Add filepath separator to confine to directory.
	authenticatedPath += string(filepath.Separator)

	// Get process of request.
	proc, _, err := process.GetProcessByConnection(ctx, pktInfo)
	if err != nil {
		log.Tracer(ctx).Debugf("filter: failed to get process of api request: %s", err)
		originalPid = process.UnidentifiedProcessID
	} else {
		originalPid = proc.Pid
		var previousPid int

		// Find parent for up to two levels, if we don't match the path.
		checkLevels := 2
	checkLevelsLoop:
		for i := 0; i < checkLevels+1; i++ {
			// Check for eligible path.
			switch proc.Pid {
			case process.UnidentifiedProcessID, process.SystemProcessID:
				break checkLevelsLoop
			default: // normal process
				// Check if the requesting process is in database root / updates dir.
				if realPath, err := filepath.EvalSymlinks(proc.Path); err == nil {
					if strings.HasPrefix(realPath, authenticatedPath) {
						return false, nil
					}
				}
			}

			// Add checked path to list.
			procsChecked = append(procsChecked, proc.Path)

			// Get the parent process.
			if i < checkLevels {
				// save previous PID
				previousPid = proc.Pid

				// get parent process
				proc, err = process.GetOrFindProcess(ctx, proc.ParentPid)
				if err != nil {
					log.Tracer(ctx).Debugf("filter: failed to get parent process of api request: %s", err)
					break
				}

				// abort if we are looping
				if proc.Pid == previousPid {
					// this also catches -1 pid loops
					break
				}
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
			authenticatedPath,
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
