package captain

import (
	"errors"
	"fmt"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/terminal"
)

var (
	ready = abool.New()

	spnLoginButton = notifications.Action{
		Text:    "登录",
		Type:    notifications.ActionTypeOpenPage,
		Payload: "spn",
	}
	spnOpenAccountPage = notifications.Action{
		Text:    "打开账户页面",
		Type:    notifications.ActionTypeOpenURL,
		Payload: "https://account.safing.io",
	}
)

// ClientReady signifies if the SPN client is fully ready to handle connections.
func ClientReady() bool {
	return ready.IsSet()
}

type (
	clientComponentFunc   func(ctx *mgr.WorkerCtx) clientComponentResult
	clientComponentResult uint8
)

const (
	clientResultOk        clientComponentResult = iota // Continue and clean module status.
	clientResultRetry                                  // Go back to start of current step, don't clear module status.
	clientResultReconnect                              // Stop current connection and start from zero.
	clientResultShutdown                               // SPN Module is shutting down.
)

var (
	clientNetworkChangedFlag               = netenv.GetNetworkChangedFlag()
	clientIneligibleAccountUpdateDelay     = 1 * time.Minute
	clientRetryConnectBackoffDuration      = 5 * time.Second
	clientInitialHealthCheckDelay          = 10 * time.Second
	clientHealthCheckTickDuration          = 1 * time.Minute
	clientHealthCheckTickDurationSleepMode = 5 * time.Minute
	clientHealthCheckTimeout               = 15 * time.Second

	clientHealthCheckTrigger = make(chan struct{}, 1)
	lastHealthCheck          time.Time
)

func triggerClientHealthCheck() {
	select {
	case clientHealthCheckTrigger <- struct{}{}:
	default:
	}
}

func clientManager(ctx *mgr.WorkerCtx) error {
	defer func() {
		ready.UnSet()
		netenv.ConnectedToSPN.UnSet()
		resetSPNStatus(StatusDisabled, true)
		module.states.Clear()
		clientStopHomeHub(ctx)
	}()

	module.states.Add(mgr.State{
		ID:      "spn:establishing-home-hub",
		Name:    "正在连接 SPN...",
		Message: "正在连接到 SPN 网络。",
		Type:    mgr.StateTypeHint,
	})

	// TODO: When we are starting and the SPN module is faster online than the
	// nameserver, then updating the account will fail as the DNS query is
	// redirected to a closed port.
	// We also can't add the nameserver as a module dependency, as the nameserver
	// is not part of the server.
	select {
	case <-time.After(1 * time.Second):
	case <-ctx.Done():
		return nil
	}

	module.healthCheckTicker = mgr.NewSleepyTicker(clientHealthCheckTickDuration, clientHealthCheckTickDurationSleepMode)
	defer module.healthCheckTicker.Stop()

reconnect:
	for {
		// Check if we are shutting down.
		if ctx.IsDone() {
			return nil
		}

		// Reset SPN status.
		if ready.SetToIf(true, false) {
			netenv.ConnectedToSPN.UnSet()
			log.Info("spn/captain: client not ready")
		}
		resetSPNStatus(StatusConnecting, true)

		// Check everything and connect to the SPN.
		for _, clientFunc := range []clientComponentFunc{
			clientStopHomeHub,
			clientCheckNetworkReady,
			clientCheckAccountAndTokens,
			clientConnectToHomeHub,
			clientSetActiveConnectionStatus,
		} {
			switch clientFunc(ctx) {
			case clientResultOk:
				// Continue
			case clientResultRetry, clientResultReconnect:
				// Wait for a short time to not loop too quickly.
				select {
				case <-time.After(clientRetryConnectBackoffDuration):
					continue reconnect
				case <-ctx.Done():
					return nil
				}
			case clientResultShutdown:
				return nil
			}
		}

		log.Info("spn/captain: client is ready")
		ready.Set()
		netenv.ConnectedToSPN.Set()

		module.EventSPNConnected.Submit(struct{}{})
		if conf.Integrated() {
			module.mgr.Go("update quick setting countries", navigator.Main.UpdateConfigQuickSettings)
		}

		// Reset last health check value, as we have just connected.
		lastHealthCheck = time.Now()

		// Back off before starting initial health checks.
		select {
		case <-time.After(clientInitialHealthCheckDelay):
		case <-ctx.Done():
			return nil
		}

		for {
			// Check health of the current SPN connection and monitor the user status.
		maintainers:
			for _, clientFunc := range []clientComponentFunc{
				clientCheckHomeHubConnection,
				clientCheckAccountAndTokens,
				clientSetActiveConnectionStatus,
			} {
				switch clientFunc(ctx) {
				case clientResultOk:
					// Continue
				case clientResultRetry:
					// Abort and wait for the next run.
					break maintainers
				case clientResultReconnect:
					continue reconnect
				case clientResultShutdown:
					return nil
				}
			}

			// Wait for signal to run maintenance again.
			select {
			case <-module.healthCheckTicker.Wait():
			case <-clientHealthCheckTrigger:
			case <-crew.ConnectErrors():
			case <-clientNetworkChangedFlag.Signal():
				clientNetworkChangedFlag.Refresh()
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func clientCheckNetworkReady(ctx *mgr.WorkerCtx) clientComponentResult {
	// Check if we are online enough for connecting.
	switch netenv.GetOnlineStatus() { //nolint:exhaustive
	case netenv.StatusOffline,
		netenv.StatusLimited:
		select {
		case <-ctx.Done():
			return clientResultShutdown
		case <-time.After(1 * time.Second):
			return clientResultRetry
		}
	}

	return clientResultOk
}

// DisableAccount disables using any account related SPN functionality.
// Attempts to use the same will result in errors.
var DisableAccount bool

func clientCheckAccountAndTokens(ctx *mgr.WorkerCtx) clientComponentResult {
	if DisableAccount {
		return clientResultOk
	}

	// Get SPN user.
	user, err := access.GetUser()
	if err != nil && !errors.Is(err, access.ErrNotLoggedIn) {
		notifications.NotifyError(
			"spn:failed-to-get-user",
			"SPN 内部错误",
			`请重新启动 Portmaster。`,
			// TODO: Add restart button.
			// TODO: Use special UI restart action in order to reload UI on restart.
		).SyncWithState(module.states)
		resetSPNStatus(StatusFailed, true)
		log.Errorf("spn/captain: client internal error: %s", err)
		return clientResultReconnect
	}

	// Check if user is logged in.
	if user == nil || !user.IsLoggedIn() {
		notifications.NotifyWarn(
			"spn:not-logged-in",
			"需要登录 SPN",
			`请登录以使用 SPN。`,
			spnLoginButton,
		).SyncWithState(module.states)
		resetSPNStatus(StatusFailed, true)
		log.Warningf("spn/captain: enabled but not logged in")
		return clientResultReconnect
	}

	// Check if user is eligible.
	if !user.MayUseTheSPN() {
		// Update user in case there was a change.
		// Only update here if we need to - there is an update task in the access
		// module for periodic updates.
		if time.Now().Add(-clientIneligibleAccountUpdateDelay).After(time.Unix(user.Meta().Modified, 0)) {
			_, _, err := access.UpdateUser()
			if err != nil {
				notifications.NotifyError(
					"spn:failed-to-update-user",
					"SPN 账户服务器错误",
					fmt.Sprintf(`无法更新您的 SPN 账户状态：%s`, err),
				).SyncWithState(module.states)
				resetSPNStatus(StatusFailed, true)
				log.Errorf("spn/captain: failed to update ineligible account: %s", err)
				return clientResultReconnect
			}
		}

		// Check if user is eligible after a possible update.
		if !user.MayUseTheSPN() {

			// If package is generally valid, then the current package does not have access to the SPN.
			if user.MayUse("") {
				notifications.NotifyError(
					"spn:package-not-eligible",
					"套餐不包含 SPN",
					"您当前的 Portmaster 套餐不包含 SPN 访问权限。请在账户页面升级您的套餐。",
					spnOpenAccountPage,
				).SyncWithState(module.states)
				resetSPNStatus(StatusFailed, true)
				return clientResultReconnect
			}

			// Otherwise, include the message from the user view.
			message := "您的 Portmaster 套餐存在问题。请检查账户页面。"
			if user.View != nil && user.View.Message != "" {
				message = user.View.Message
			}
			notifications.NotifyError(
				"spn:subscription-inactive",
				"Portmaster 套餐问题",
				"无法启用 SPN："+message,
				spnOpenAccountPage,
			).SyncWithState(module.states)
			resetSPNStatus(StatusFailed, true)
			return clientResultReconnect
		}
	}

	// Check if we have enough tokens.
	if access.ShouldRequest(access.ExpandAndConnectZones) {
		err := access.UpdateTokens()
		if err != nil {
			log.Errorf("spn/captain: failed to get tokens: %s", err)

			// There was an error updating the account.
			// Check if we have enough tokens to continue anyway.
			regular, _ := access.GetTokenAmount(access.ExpandAndConnectZones)
			if regular == 0 /* && fallback == 0 */ { // TODO: Add fallback token check when fallback was tested on servers.
				notifications.NotifyError(
					"spn:tokens-exhausted",
					"SPN 访问令牌已耗尽",
					`Portmaster 未能获取用于访问 SPN 的新访问令牌。Portmaster 将自动重试获取新的访问令牌。`,
				).SyncWithState(module.states)
				resetSPNStatus(StatusFailed, false)
			}
			return clientResultRetry
		}
	}

	return clientResultOk
}

func clientStopHomeHub(ctx *mgr.WorkerCtx) clientComponentResult {
	// Don't use the context in this function, as it will likely be canceled
	// already and would disrupt any context usage in here.

	// Get crane connecting to home.
	home, _ := navigator.Main.GetHome()
	if home == nil {
		return clientResultOk
	}
	crane := docks.GetAssignedCrane(home.Hub.ID)
	if crane == nil {
		return clientResultOk
	}

	// Stop crane and all connected terminals.
	crane.Stop(nil)
	return clientResultOk
}

func clientConnectToHomeHub(ctx *mgr.WorkerCtx) clientComponentResult {
	err := establishHomeHub(ctx)
	if err != nil {
		if ctx.IsDone() {
			return clientResultShutdown
		}

		log.Errorf("spn/captain: failed to establish connection to home hub: %s", err)
		resetSPNStatus(StatusFailed, true)

		switch {
		case errors.Is(err, ErrAllHomeHubsExcluded):
			notifications.NotifyError(
				"spn:all-home-hubs-excluded",
				"所有主节点均被排除",
				"您当前的主节点规则排除了所有可用且符合条件的 SPN 节点。请修改您的规则，以至少允许一个可用且符合条件的主节点。",
				notifications.Action{
					Text: "配置",
					Type: notifications.ActionTypeOpenSetting,
					Payload: &notifications.ActionTypeOpenSettingPayload{
						Key: CfgOptionHomeHubPolicyKey,
					},
				},
			).SyncWithState(module.states)

		case errors.Is(err, ErrReInitSPNSuggested):
			notifications.NotifyError(
				"spn:cannot-bootstrap",
				"SPN 无法引导",
				"SPN 网络的本地状态可能已过时。Portmaster 无法找到可连接的服务器。请使用工具菜单或通知上的按钮重新初始化 SPN。",
				notifications.Action{
					ID:   "re-init",
					Text: "重新初始化 SPN",
					Type: notifications.ActionTypeWebhook,
					Payload: &notifications.ActionTypeWebhookPayload{
						URL:          apiPathForSPNReInit,
						ResultAction: "display",
					},
				},
			).SyncWithState(module.states)

		default:
			notifications.NotifyWarn(
				"spn:home-hub-failure",
				"SPN 连接失败",
				fmt.Sprintf("连接主节点失败：%s。Portmaster 将自动重试连接。", err),
			).SyncWithState(module.states)
		}

		return clientResultReconnect
	}

	// Log new connection.
	home, _ := navigator.Main.GetHome()
	if home != nil {
		log.Infof("spn/captain: established new home %s", home.Hub)
	}

	return clientResultOk
}

func clientSetActiveConnectionStatus(ctx *mgr.WorkerCtx) clientComponentResult {
	// Get current home.
	home, homeTerminal := navigator.Main.GetHome()
	if home == nil || homeTerminal == nil {
		return clientResultReconnect
	}

	// Resolve any connection error.
	module.states.Clear()

	// Update SPN Status with connection information, if not already correctly set.
	spnStatus.Lock()
	defer spnStatus.Unlock()

	if spnStatus.Status != StatusConnected || spnStatus.HomeHubID != home.Hub.ID {
		// Fill connection status data.
		spnStatus.Status = StatusConnected
		spnStatus.HomeHubID = home.Hub.ID
		spnStatus.HomeHubName = home.Hub.Info.Name

		connectedIP, _, err := netutils.IPPortFromAddr(homeTerminal.RemoteAddr())
		if err != nil {
			spnStatus.ConnectedIP = homeTerminal.RemoteAddr().String()
		} else {
			spnStatus.ConnectedIP = connectedIP.String()
		}
		spnStatus.ConnectedTransport = homeTerminal.Transport().String()

		geoLoc := home.GetLocation(connectedIP)
		if geoLoc != nil {
			spnStatus.ConnectedCountry = &geoLoc.Country
		}

		now := time.Now()
		spnStatus.ConnectedSince = &now

		// Push new status.
		pushSPNStatusUpdate()
	}

	return clientResultOk
}

func clientCheckHomeHubConnection(ctx *mgr.WorkerCtx) clientComponentResult {
	// Check the status of the Home Hub.
	home, homeTerminal := navigator.Main.GetHome()
	if home == nil || homeTerminal == nil || homeTerminal.IsBeingAbandoned() {
		return clientResultReconnect
	}

	// Get crane controller for health check.
	crane := docks.GetAssignedCrane(home.Hub.ID)
	if crane == nil {
		log.Errorf("spn/captain: could not find home hub crane for health check")
		return clientResultOk
	}

	// Ping home hub.
	latency, tErr := pingHome(ctx, crane.Controller, clientHealthCheckTimeout)
	if tErr != nil {
		log.Warningf("spn/captain: failed to ping home hub: %s", tErr)

		// Prepare to reconnect to the network.

		// Reset all failing states, as these might have been caused by the failing home hub.
		navigator.Main.ResetFailingStates()

		// If the last health check is clearly too long ago, assume that the device was sleeping and do not set the home node to failing yet.
		if time.Since(lastHealthCheck) > clientHealthCheckTickDuration+
			clientHealthCheckTickDurationSleepMode+
			(clientHealthCheckTimeout*2) {
			return clientResultReconnect
		}

		// Mark the home hub itself as failing, as we want to try to connect to somewhere else.
		home.MarkAsFailingFor(5 * time.Minute)

		return clientResultReconnect
	}
	lastHealthCheck = time.Now()

	log.Debugf("spn/captain: pinged home hub in %s", latency)
	return clientResultOk
}

func pingHome(ctx *mgr.WorkerCtx, t terminal.Terminal, timeout time.Duration) (latency time.Duration, err *terminal.Error) {
	started := time.Now()

	// Start ping operation.
	pingOp, tErr := crew.NewPingOp(t)
	if tErr != nil {
		return 0, tErr
	}

	// Wait for response.
	select {
	case <-ctx.Done():
		return 0, terminal.ErrCanceled
	case <-time.After(timeout):
		return 0, terminal.ErrTimeout
	case result := <-pingOp.Result:
		if result.Is(terminal.ErrExplicitAck) {
			return time.Since(started), nil
		}
		if result.IsOK() {
			return 0, result.Wrap("unexpected response")
		}
		return 0, result
	}
}
