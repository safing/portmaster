// Package plugin provides an plugin and extension system for the
// Portmaster.
//
// The plugin system is based on github.com/hashicorp/go-plugin and
// uses a sub-process architecture where the plugin host (the Portmaster)
// and plugins communicate via gRPC on a localhost HTTP/2 connection.
//
// The package defines a couple of different types that plugin authors may
// implement depending on their planned feature set:
//
// - Decider Plugins:
//	 A decider plugin is used by the firewall system to decide if a connection
//   or DNS request is allowed, blocked or routed through the SPN.
//
// - Reporter Plugins:
//   A reporter plugin is notified whenever the firewall found a decision
//	 on a new connection or DNS request. Reporter plugins may be used to store
//   connection history in places not directly supported by the Portmaster.
//
// In addition to the plugin types available plugins also have access to the
// following Portmaster systems:
//
// - Config System:
//   Plugins can register custom configuration options that will show up in the
//   Portmaster user interface and can watch for changes to those configuration options.
//
// - Notification System:
//   Plugins may display custom notifications to the user with support for notification
//   actions. Plugins may also "take-over" notifications and can present the to the user
//   different ways (like pushing to a mobile phone).
//   Plugin developers must make sure to not take-over notifications whose defined actions
//   cannot be supported by the plugin implementation. That is, most actions defined in the
//   proto package are meant to be displayed and executed by the User Interface. An
//   exception to this, for example, is the Webhook action which may easily implemented
//   by plugins as well.
//
// For simple example plugins please refer the the plugin-examples repository
// in github.com/safing/plugin-examples.
package plugin
