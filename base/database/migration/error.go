package migration

import "errors"

// DiagnosticStep describes one migration step in the Diagnostics.
type DiagnosticStep struct {
	Version     string
	Description string
}

// Diagnostics holds a detailed error report about a failed migration.
type Diagnostics struct { //nolint:errname
	// Message holds a human readable message of the encountered
	// error.
	Message string
	// Wrapped must be set to the underlying error that was encountered
	// while preparing or executing migrations.
	Wrapped error
	// StartOfMigration is set to the version of the database before
	// any migrations are applied.
	StartOfMigration string
	// LastSuccessfulMigration is set to the version of the database
	// which has been applied successfully before the error happened.
	LastSuccessfulMigration string
	// TargetVersion is set to the version of the database that the
	// migration run aimed for. That is, it's the last available version
	// added to the registry.
	TargetVersion string
	// ExecutionPlan is a list of migration steps that were planned to
	// be executed.
	ExecutionPlan []DiagnosticStep
	// FailedMigration is the description of the migration that has
	// failed.
	FailedMigration string
}

// Error returns a string representation of the migration error.
func (err *Diagnostics) Error() string {
	msg := ""
	if err.FailedMigration != "" {
		msg = err.FailedMigration + ": "
	}
	if err.Message != "" {
		msg += err.Message + ": "
	}
	msg += err.Wrapped.Error()
	return msg
}

// Unwrap returns the actual error that happened when executing
// a migration. It implements the interface required by the stdlib
// errors package to support errors.Is() and errors.As().
func (err *Diagnostics) Unwrap() error {
	if u := errors.Unwrap(err.Wrapped); u != nil {
		return u
	}
	return err.Wrapped
}
