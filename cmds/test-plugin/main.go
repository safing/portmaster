package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/safing/portmaster/plugin/framework"
	"github.com/safing/portmaster/plugin/shared/proto"
	"github.com/spf13/cobra"
)

var (
	decider  = new(TestDeciderPlugin)
	reporter = new(TestReporterPlugin)
)

type checkResult struct {
	name string
	err  error
}

func getRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "test-plugin",
		Run: func(cmd *cobra.Command, args []string) {
			// Create a new decider plugin implementation and register it
			// at the framework
			framework.RegisterDecider(decider)

			// Create a new reporter plugin implementation and register it
			// at the framework
			framework.RegisterReporter(reporter)

			// Once the framework is initialized we can start doing our
			// tests.
			framework.OnInit(func(ctx context.Context) error {
				decision, err := framework.Notifications().CreateNotification(
					ctx,
					&proto.Notification{
						EventId: "test-plugin:launch-test",
						Title:   "Launch Plugin Tests?",
						Message: "The test plugin is installed, ready to launch tests?",
						Actions: []*proto.NotificationAction{
							{
								Id:   "start",
								Text: "Launch Tests",
							},
							{
								Id:   "not-now",
								Text: "Not Now",
							},
						},
					},
				)
				if err != nil {
					return err
				}

				// wait for the response
				res, ok := <-decision
				if !ok {
					return fmt.Errorf("user did not respond to notification")
				}

				if res == "start" {
					go launchTests()
				}
				return nil
			})

			// Finally, actually serve the plugin
			framework.Serve()
		},
	}

	cmd.AddCommand(
		getInstallCmd(),
	)

	return cmd
}

func createTestRunNotification(results []checkResult) {
	msg := ""

	for _, result := range results {
		if result.err == nil {
			msg += "**[PASSED]** " + result.name
		} else {
			msg += "**[ FAIL ]** " + result.name + ": " + result.err.Error()
		}

		msg += "\n"
	}
	_, err := framework.Notifications().CreateNotification(context.Background(), &proto.Notification{
		EventId: "test-plugin:tests-launched",
		Title:   "Plugin Tests in Progess",
		Message: msg,
	})
	if err != nil {
		log.Fatalf("failed to create notification: %s", err)
	}
}

func createTestFinishedNotification() {
	_, err := framework.Notifications().CreateNotification(context.Background(), &proto.Notification{
		EventId: "test-plugin:tests-launched",
		Title:   "Plugin Tests in Progess",
		Message: "Plugin tests are being executed. Please be patient",
	})
	if err != nil {
		log.Fatalf("failed to create notification: %s", err)
	}
}

func launchTests() {
	createTestRunNotification(nil)

	time.Sleep(10 * time.Second)

	createTestFinishedNotification()
}

func main() {
	if err := getRootCmd().Execute(); err != nil {
		log.Fatalf("failed to run: %s", err)
	}
}
