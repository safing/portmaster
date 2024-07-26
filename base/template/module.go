package template

// FIXME: update template to new best way to do it.
// import (
// 	"context"
// 	"time"

// 	"github.com/safing/portmaster/base/config"
// )

// const (
// 	eventStateUpdate = "state update"
// )

// var module *modules.Module

// func init() {
// 	// register module
// 	module = modules.Register("template", prep, start, stop) // add dependencies...
// 	subsystems.Register(
// 		"template-subsystem",                           // ID
// 		"Template Subsystem",                           // name
// 		"This subsystem is a template for quick setup", // description
// 		module,
// 		"config:template", // key space for configuration options registered
// 		&config.Option{
// 			Name:         "Template Subsystem",
// 			Key:          "config:subsystems/template",
// 			Description:  "This option enables the Template Subsystem [TEMPLATE]",
// 			OptType:      config.OptTypeBool,
// 			DefaultValue: false,
// 		},
// 	)

// 	// register events that other modules can subscribe to
// 	module.RegisterEvent(eventStateUpdate, true)
// }

// func prep() error {
// 	// register options
// 	err := config.Register(&config.Option{
// 		Name:            "language",
// 		Key:             "template/language",
// 		Description:     "Sets the language for the template [TEMPLATE]",
// 		OptType:         config.OptTypeString,
// 		ExpertiseLevel:  config.ExpertiseLevelUser, // default
// 		ReleaseLevel:    config.ReleaseLevelStable, // default
// 		RequiresRestart: false,                     // default
// 		DefaultValue:    "en",
// 		ValidationRegex: "^[a-z]{2}$",
// 	})
// 	if err != nil {
// 		return err
// 	}

// 	// register event hooks
// 	// do this in prep() and not in start(), as we don't want to register again if module is turned off and on again
// 	err = module.RegisterEventHook(
// 		"template",               // event source module name
// 		"state update",           // event source name
// 		"react to state changes", // description of hook function
// 		eventHandler,             // hook function
// 	)
// 	if err != nil {
// 		return err
// 	}

// 	// hint: event hooks and tasks will not be run if module isn't online
// 	return nil
// }

// func start() error {
// 	// register tasks
// 	module.NewTask("do something", taskFn).Queue()

// 	// start service worker
// 	module.StartServiceWorker("do something", 0, serviceWorker)

// 	return nil
// }

// func stop() error {
// 	return nil
// }

// func serviceWorker(ctx context.Context) error {
// 	for {
// 		select {
// 		case <-time.After(1 * time.Second):
// 			err := do()
// 			if err != nil {
// 				return err
// 			}
// 		case <-ctx.Done():
// 			return nil
// 		}
// 	}
// }

// func taskFn(ctx context.Context, task *modules.Task) error {
// 	return do()
// }

// func eventHandler(ctx context.Context, data interface{}) error {
// 	return do()
// }

// func do() error {
// 	return nil
// }
