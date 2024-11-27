package network

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/network/state"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/portmaster/service/updates"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "debug/network",
		Read:        api.PermitUser,
		DataFunc:    debugInfo,
		Name:        "Get Network Debug Information",
		Description: "Returns network debugging information, similar to debug/core, but with connection data.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "style",
				Value:       "github",
				Description: "Specify the formatting style. The default is simple markdown formatting.",
			},
			{
				Method:      http.MethodGet,
				Field:       "profile",
				Value:       "<Source>/<ID>",
				Description: "Specify a profile source and ID for which network connection should be reported.",
			},
			{
				Method:      http.MethodGet,
				Field:       "where",
				Value:       "<query>",
				Description: "Specify a query to limit the connections included in the report. The default is to include all connections.",
			},
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "debug/network/state",
		Read: api.PermitUser,
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return state.GetInfo(), nil
		},
		Name:        "Get Network State Table Data",
		Description: "Returns the current network state tables from the OS.",
	}); err != nil {
		return err
	}

	return nil
}

// debugInfo returns the debugging information for support requests.
func debugInfo(ar *api.Request) (data []byte, err error) {
	// Create debug information helper.
	di := new(debug.Info)
	di.Style = ar.Request.URL.Query().Get("style")

	// Add debug information.

	// Very basic information at the start.
	di.AddVersionInfo()
	di.AddPlatformInfo(ar.Context())

	// Unexpected logs.
	di.AddLastUnexpectedLogs()

	// Network Connections.
	AddNetworkDebugData(
		di,
		ar.Request.URL.Query().Get("profile"),
		ar.Request.URL.Query().Get("where"),
	)

	// Status Information from various modules.
	status.AddToDebugInfo(di)
	// captain.AddToDebugInfo(di) // TODO: Cannot use due to import loop.
	resolver.AddToDebugInfo(di)
	config.AddToDebugInfo(di)

	// Detailed information.
	updates.AddToDebugInfo(di)
	// compat.AddToDebugInfo(di) // TODO: Cannot use due to interception import requirement which we don't want for SPN Hubs.
	di.AddGoroutineStack()

	// Return data.
	return di.Bytes(), nil
}

// AddNetworkDebugData adds the network debug data of the given profile to the debug data.
func AddNetworkDebugData(di *debug.Info, profile, where string) {
	// Prepend where prefix to query if necessary.
	if where != "" && !strings.HasPrefix(where, "where ") {
		where = "where " + where
	}

	// Build query.
	q, err := query.ParseQuery("query network: " + where)
	if err != nil {
		di.AddSection(
			"Network: Debug Failed",
			debug.NoFlags,
			fmt.Sprintf("Failed to build query: %s", err),
		)
		return
	}

	// Get iterator.
	it, err := dbController.Query(q, true, true)
	if err != nil {
		di.AddSection(
			"Network: Debug Failed",
			debug.NoFlags,
			fmt.Sprintf("Failed to run query: %s", err),
		)
		return
	}

	// Collect matching connections.
	var ( //nolint:prealloc // We don't know the size.
		debugConns []*Connection
		accepted   int
		total      int
	)

	for maybeConn := range it.Next {
		// Switch to correct type.
		conn, ok := maybeConn.(*Connection)
		if !ok {
			continue
		}

		// Check if the profile matches
		if profile != "" {
			found := false

			// Get layer IDs and search for a match.
			layerIDs := conn.Process().Profile().LayerIDs
			for _, layerID := range layerIDs {
				if profile == layerID {
					found = true
					break
				}
			}

			// Skip if the profile does not match.
			if !found {
				continue
			}
		}

		// Count.
		total++
		switch conn.Verdict { //nolint:exhaustive
		case VerdictAccept,
			VerdictRerouteToNameserver,
			VerdictRerouteToTunnel:

			accepted++
		}

		// Add to list.
		debugConns = append(debugConns, conn)
	}

	// Add it all.
	di.AddSection(
		fmt.Sprintf(
			"Network: %d/%d Connections",
			accepted,
			total,
		),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		buildNetworkDebugInfoData(debugConns),
	)
}

func buildNetworkDebugInfoData(debugConns []*Connection) string {
	// Sort
	sort.Sort(connectionsByGroup(debugConns))

	// Format lines
	var buf strings.Builder
	currentPID := process.UndefinedProcessID
	for _, conn := range debugConns {
		conn.Lock()

		// Add process infomration if it differs from previous connection.
		if currentPID != conn.ProcessContext.PID {
			if currentPID != process.UndefinedProcessID {
				buf.WriteString("\n\n\n")
			}
			buf.WriteString("ProfileName: " + conn.ProcessContext.ProfileName)
			buf.WriteString("\nProfile:     " + conn.ProcessContext.Profile)
			buf.WriteString("\nSource:      " + conn.ProcessContext.Source)
			buf.WriteString("\nProcessName: " + conn.ProcessContext.ProcessName)
			buf.WriteString("\nBinaryPath:  " + conn.ProcessContext.BinaryPath)
			buf.WriteString("\nCmdLine:     " + conn.ProcessContext.CmdLine)
			buf.WriteString("\nPID:         " + strconv.Itoa(conn.ProcessContext.PID))
			buf.WriteString("\n")

			// Set current PID in order to not print the process information again.
			currentPID = conn.ProcessContext.PID
		}

		// Add connection.
		buf.WriteString("\n")
		buf.WriteString(conn.debugInfoLine())

		conn.Unlock()
	}

	return buf.String()
}

func (conn *Connection) debugInfoLine() string {
	var connectionData string
	if conn.Type == IPConnection {
		// Format IP/Port pair for connections.
		connectionData = fmt.Sprintf(
			"% 15s:%- 5s %s % 15s:%- 5s",
			conn.LocalIP,
			strconv.Itoa(int(conn.LocalPort)),
			conn.fmtProtocolAndDirectionComponent(conn.IPProtocol.String()),
			conn.Entity.IP,
			strconv.Itoa(int(conn.Entity.Port)),
		)
	} else {
		// Leave empty for DNS Requests.
		connectionData = "                                                "
	}

	return fmt.Sprintf(
		"% 14s %s%- 25s %s-%s P#%d [%s] %s - by %s @ %s",
		conn.VerdictVerb(),
		connectionData,
		conn.fmtDomainComponent(),
		time.Unix(conn.Started, 0).Format("15:04:05"),
		conn.fmtEndTimeComponent(),
		conn.ProcessContext.PID,
		conn.fmtFlagsComponent(),
		conn.Reason.Msg,
		conn.Reason.OptionKey,
		conn.fmtReasonProfileComponent(),
	)
}

func (conn *Connection) fmtDomainComponent() string {
	if conn.Entity.Domain != "" {
		return " to " + conn.Entity.Domain
	}
	return ""
}

func (conn *Connection) fmtProtocolAndDirectionComponent(protocol string) string {
	if conn.Inbound {
		return "<" + protocol
	}
	return protocol + ">"
}

func (conn *Connection) fmtFlagsComponent() string {
	var f string

	if conn.Internal {
		f += "I"
	}
	if conn.Encrypted {
		f += "E"
	}
	if conn.Tunneled {
		f += "T"
	}
	if len(conn.activeInspectors) > 0 {
		f += "A"
	}
	if conn.addedToMetrics {
		f += "M"
	}

	return f
}

func (conn *Connection) fmtEndTimeComponent() string {
	if conn.Ended == 0 {
		return "        " // Use same width as a timestamp.
	}
	return time.Unix(conn.Ended, 0).Format("15:04:05")
}

func (conn *Connection) fmtReasonProfileComponent() string {
	if conn.Reason.Profile == "" {
		return "global"
	}
	return conn.Reason.Profile
}

type connectionsByGroup []*Connection

func (a connectionsByGroup) Len() int      { return len(a) }
func (a connectionsByGroup) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a connectionsByGroup) Less(i, j int) bool {
	// Sort by:

	// 1. Profile ID
	if a[i].ProcessContext.Profile != a[j].ProcessContext.Profile {
		return a[i].ProcessContext.Profile < a[j].ProcessContext.Profile
	}

	// 2. Process Binary
	if a[i].ProcessContext.BinaryPath != a[j].ProcessContext.BinaryPath {
		return a[i].ProcessContext.BinaryPath < a[j].ProcessContext.BinaryPath
	}

	// 3. Process ID
	if a[i].ProcessContext.PID != a[j].ProcessContext.PID {
		return a[i].ProcessContext.PID < a[j].ProcessContext.PID
	}

	// 4. Started
	return a[i].Started < a[j].Started
}
