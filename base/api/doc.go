/*
Package api provides an API for integration with other components of the same software package and also third party components.

It provides direct database access as well as a simpler way to register API endpoints. You can of course also register raw `http.Handler`s directly.

Optional authentication guards registered handlers. This is achieved by attaching functions to the `http.Handler`s that are registered, which allow them to specify the required permissions for the handler.

The permissions are divided into the roles and assume a single user per host. The Roles are User, Admin and Self. User roles are expected to have mostly read access and react to notifications or system events, like a system tray program. The Admin role is meant for advanced components that also change settings, but are restricted so they cannot break the software. Self is reserved for internal use with full access.
*/
package api
