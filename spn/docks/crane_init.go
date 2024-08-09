package docks

import (
	"context"
	"time"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

/*

Crane Init Message Format:
used by init procedures

- Data [bytes block]
	- MsgType [varint]
	- Data [bytes; only when MsgType is Verify or Start*]

Crane Init Response Format:

- Data [bytes block]

Crane Operational Message Format:

- Data [bytes block]
	- possibly encrypted

*/

// Crane Msg Types.
const (
	CraneMsgTypeEnd              = 0
	CraneMsgTypeInfo             = 1
	CraneMsgTypeRequestHubInfo   = 2
	CraneMsgTypeVerify           = 3
	CraneMsgTypeStartEncrypted   = 4
	CraneMsgTypeStartUnencrypted = 5
)

// Start starts the crane.
func (crane *Crane) Start(callerCtx context.Context) error {
	log.Infof("spn/docks: %s is starting", crane)

	// Submit metrics.
	newCranes.Inc()

	// Start crane depending on situation.
	var tErr *terminal.Error
	if crane.ship.IsMine() {
		tErr = crane.startLocal(callerCtx)
	} else {
		tErr = crane.startRemote(callerCtx)
	}

	// Stop crane again if starting failed.
	if tErr != nil {
		crane.Stop(tErr)
		return tErr
	}

	log.Debugf("spn/docks: %s started", crane)
	// Return an explicit nil for working "!= nil" checks.
	return nil
}

func (crane *Crane) startLocal(callerCtx context.Context) *terminal.Error {
	module.mgr.Go("crane unloader", crane.unloader)

	if !crane.ship.IsSecure() {
		// Start encrypted channel.
		// Check if we have all the data we need from the Hub.
		if crane.ConnectedHub == nil {
			return terminal.ErrIncorrectUsage.With("cannot start encrypted channel without connected hub")
		}

		// Always request hub info, as we don't know if the hub has restarted in
		// the meantime and lost ephemeral keys.
		hubInfoRequest := container.New(
			varint.Pack8(CraneMsgTypeRequestHubInfo),
		)
		hubInfoRequest.PrependLength()
		err := crane.ship.Load(hubInfoRequest.CompileData())
		if err != nil {
			return terminal.ErrShipSunk.With("failed to request hub info: %w", err)
		}

		// Wait for reply.
		var reply *container.Container
		select {
		case reply = <-crane.unloading:
		case <-time.After(30 * time.Second):
			return terminal.ErrTimeout.With("waiting for hub info")
		case <-crane.ctx.Done():
			return terminal.ErrShipSunk.With("waiting for hub info")
		case <-callerCtx.Done():
			return terminal.ErrCanceled.With("waiting for hub info")
		}

		// Parse and import Announcement and Status.
		announcementData, err := reply.GetNextBlock()
		if err != nil {
			return terminal.ErrMalformedData.With("failed to get announcement: %w", err)
		}
		statusData, err := reply.GetNextBlock()
		if err != nil {
			return terminal.ErrMalformedData.With("failed to get status: %w", err)
		}
		h, _, tErr := ImportAndVerifyHubInfo(
			callerCtx,
			crane.ConnectedHub.ID,
			announcementData, statusData, conf.MainMapName, conf.MainMapScope,
		)
		if tErr != nil {
			return tErr.Wrap("failed to import and verify hub")
		}
		// Update reference in case it was changed by the import.
		crane.ConnectedHub = h

		// Now, try to select a public key again.
		signet := crane.ConnectedHub.SelectSignet()
		if signet == nil {
			return terminal.ErrHubNotReady.With("failed to select signet (after updating hub info)")
		}

		// Configure encryption.
		env := jess.NewUnconfiguredEnvelope()
		env.SuiteID = jess.SuiteWireV1
		env.Recipients = []*jess.Signet{signet}

		// Do not encrypt directly, rather get session for future use, then encrypt.
		crane.jession, err = env.WireCorrespondence(nil)
		if err != nil {
			return terminal.ErrInternalError.With("failed to create encryption session: %w", err)
		}
	}

	// Create crane controller.
	_, initData, tErr := NewLocalCraneControllerTerminal(crane, terminal.DefaultCraneControllerOpts())
	if tErr != nil {
		return tErr.Wrap("failed to set up controller")
	}

	// Prepare init message for sending.
	if crane.ship.IsSecure() {
		initData.PrependNumber(CraneMsgTypeStartUnencrypted)
	} else {
		// Encrypt controller initializer.
		letter, err := crane.jession.Close(initData.CompileData())
		if err != nil {
			return terminal.ErrInternalError.With("failed to encrypt initial packet: %w", err)
		}
		initData, err = letter.ToWire()
		if err != nil {
			return terminal.ErrInternalError.With("failed to pack initial packet: %w", err)
		}
		initData.PrependNumber(CraneMsgTypeStartEncrypted)
	}

	// Send start message.
	initData.PrependLength()
	err := crane.ship.Load(initData.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send init msg: %w", err)
	}

	// Start remaining workers.
	module.mgr.Go("crane loader", crane.loader)
	module.mgr.Go("crane handler", crane.handler)

	return nil
}

func (crane *Crane) startRemote(callerCtx context.Context) *terminal.Error {
	var initMsg *container.Container

	module.mgr.Go("crane unloader", crane.unloader)

handling:
	for {
		// Wait for request.
		var request *container.Container
		select {
		case request = <-crane.unloading:

		case <-time.After(30 * time.Second):
			return terminal.ErrTimeout.With("waiting for crane init msg")
		case <-crane.ctx.Done():
			return terminal.ErrShipSunk.With("waiting for crane init msg")
		case <-callerCtx.Done():
			return terminal.ErrCanceled.With("waiting for crane init msg")
		}

		msgType, err := request.GetNextN8()
		if err != nil {
			return terminal.ErrMalformedData.With("failed to parse crane msg type: %s", err)
		}

		switch msgType {
		case CraneMsgTypeEnd:
			// End connection.
			return terminal.ErrStopping

		case CraneMsgTypeInfo:
			// Info is a terminating request.
			err := crane.handleCraneInfo()
			if err != nil {
				return err
			}
			log.Debugf("spn/docks: %s sent version info", crane)

		case CraneMsgTypeRequestHubInfo:
			// Handle Hub info request.
			err := crane.handleCraneHubInfo()
			if err != nil {
				return err
			}
			log.Debugf("spn/docks: %s sent hub info", crane)

		case CraneMsgTypeVerify:
			// Verify is a terminating request.
			err := crane.handleCraneVerification(request)
			if err != nil {
				return err
			}
			log.Infof("spn/docks: %s sent hub verification", crane)

		case CraneMsgTypeStartUnencrypted:
			initMsg = request

			// Start crane with initMsg.
			log.Debugf("spn/docks: %s initiated unencrypted channel", crane)
			break handling

		case CraneMsgTypeStartEncrypted:
			if crane.identity == nil {
				return terminal.ErrIncorrectUsage.With("cannot start incoming crane without designated identity")
			}

			// Set up encryption.
			letter, err := jess.LetterFromWire(container.New(request.CompileData()))
			if err != nil {
				return terminal.ErrMalformedData.With("failed to unpack initial packet: %w", err)
			}
			crane.jession, err = letter.WireCorrespondence(crane.identity)
			if err != nil {
				return terminal.ErrInternalError.With("failed to create encryption session: %w", err)
			}
			initMsgData, err := crane.jession.Open(letter)
			if err != nil {
				return terminal.ErrIntegrity.With("failed to decrypt initial packet: %w", err)
			}
			initMsg = container.New(initMsgData)

			// Start crane with initMsg.
			log.Debugf("spn/docks: %s initiated encrypted channel", crane)
			break handling
		}
	}

	_, _, err := NewRemoteCraneControllerTerminal(crane, initMsg)
	if err != nil {
		return err.Wrap("failed to start crane controller")
	}

	// Start remaining workers.
	module.mgr.Go("crane loader", crane.loader)
	module.mgr.Go("crane handler", crane.handler)

	return nil
}

func (crane *Crane) endInit() *terminal.Error {
	endMsg := container.New(
		varint.Pack8(CraneMsgTypeEnd),
	)
	endMsg.PrependLength()
	err := crane.ship.Load(endMsg.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send end msg: %w", err)
	}
	return nil
}

func (crane *Crane) handleCraneInfo() *terminal.Error {
	// Pack info data.
	infoData, err := dsd.Dump(info.GetInfo(), dsd.JSON)
	if err != nil {
		return terminal.ErrInternalError.With("failed to pack info: %w", err)
	}
	msg := container.New(infoData)

	// Manually send reply.
	msg.PrependLength()
	err = crane.ship.Load(msg.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send info reply: %w", err)
	}

	return nil
}

func (crane *Crane) handleCraneHubInfo() *terminal.Error {
	msg := container.New()

	// Check if we have an identity.
	if crane.identity == nil {
		return terminal.ErrIncorrectUsage.With("cannot handle hub info request without designated identity")
	}

	// Add Hub Announcement.
	announcementData, err := crane.identity.ExportAnnouncement()
	if err != nil {
		return terminal.ErrInternalError.With("failed to export announcement: %w", err)
	}
	msg.AppendAsBlock(announcementData)

	// Add Hub Status.
	statusData, err := crane.identity.ExportStatus()
	if err != nil {
		return terminal.ErrInternalError.With("failed to export status: %w", err)
	}
	msg.AppendAsBlock(statusData)

	// Manually send reply.
	msg.PrependLength()
	err = crane.ship.Load(msg.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send hub info reply: %w", err)
	}

	return nil
}
