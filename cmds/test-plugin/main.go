package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/safing/portmaster/plugin/framework"
	"github.com/safing/portmaster/plugin/framework/cmds"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
	"github.com/spf13/cobra"
)

type checkResult struct {
	name   string
	passed bool
	err    error
}

var (
	decider  = new(TestDeciderPlugin)
	reporter = new(TestReporterPlugin)

	resultsLock sync.Mutex
	results     []*checkResult
)

func getRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
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
				decision, err := framework.Notify().CreateNotification(
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

	rootCmd.AddCommand(
		cmds.InstallCommand(&cmds.InstallCommandConfig{
			PluginName: "test-plugin",
			Types: []shared.PluginType{
				shared.PluginTypeDecider,
				shared.PluginTypeReporter,
			},
		}),
	)

	return rootCmd
}

func createTestRunNotification() {
	msg := ""

	resultsLock.Lock()
	defer resultsLock.Unlock()

	for _, result := range results {
		if result.err == nil {
			if result.passed {
				msg += ":heavy_check_mark: " + result.name
			} else {
				msg += ":running: " + result.name
			}
		} else {
			msg += ":x: " + result.name + ": " + result.err.Error()
		}

		msg += "  \n"
	}
	_, err := framework.Notify().CreateNotification(framework.Context(), &proto.Notification{
		EventId: "test-plugin:tests-launched",
		Title:   "Plugin Tests in Progess",
		Message: msg,
	})
	if err != nil {
		log.Fatalf("failed to create notification: %s", err)
	}
}

func createTestFinishedNotification() {
	_, err := framework.Notify().CreateNotification(framework.Context(), &proto.Notification{
		EventId: "test-plugin:tests-launched",
		Title:   "Plugin Tests in Progess",
		Message: "Plugin tests are being executed. Please be patient",
	})
	if err != nil {
		log.Fatalf("failed to create notification: %s", err)
	}
}

func launchTests() {
	RunTest("Reporter is called for connections", TestReporterIsCalled)
	RunTest("Decider is called for connections", TestDeciderIsCalled)
	RunTest("Blocking deciders should be ignored", TestBlockingDecider)
}

func main() {
	if err := getRootCmd().Execute(); err != nil {
		log.Fatalf("failed to run: %s", err)
	}
}
