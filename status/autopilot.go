package status

import "context"

var runAutoPilot = make(chan struct{})

func triggerAutopilot() {
	select {
	case runAutoPilot <- struct{}{}:
	default:
	}
}

func autoPilot(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-runAutoPilot:
		}

		selected := SelectedSecurityLevel()
		mitigation := getHighestMitigationLevel()

		active := SecurityLevelNormal
		if selected != SecurityLevelOff {
			active = selected
		} else if mitigation != SecurityLevelOff {
			active = mitigation
		}

		setActiveLevel(active)

		pushSystemStatus()
	}
}
