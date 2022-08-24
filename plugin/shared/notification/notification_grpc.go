package notification

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	GRPCClient struct {
		Client proto.NotificationServiceClient
	}

	GRPCServer struct {
		proto.UnimplementedNotificationServiceServer

		Impl       Service
		PluginName string
	}
)

func (cli *GRPCClient) CreateNotification(ctx context.Context, notif *proto.Notification) (<-chan string, error) {
	stream, err := cli.Client.CreateNotification(ctx, &proto.CreateNotificationRequest{
		Notification: notif,
	})

	if err != nil {
		return nil, err
	}

	ch := make(chan string)

	go func() {
		defer close(ch)

		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}

			ch <- msg.SelectedActionId
		}
	}()

	return ch, nil
}

func (srv *GRPCServer) CreateNotification(req *proto.CreateNotificationRequest, stream proto.NotificationService_CreateNotificationServer) error {
	ch, err := srv.Impl.CreateNotification(stream.Context(), req.Notification)
	if err != nil {
		return err
	}

	for msg := range ch {
		if err := stream.Send(&proto.CreateNotificationResponse{
			SelectedActionId: msg,
		}); err != nil {
			return err
		}
	}

	return nil
}
