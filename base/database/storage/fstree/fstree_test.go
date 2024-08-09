package fstree

import "github.com/safing/portmaster/base/database/storage"

// Compile time interface checks.
var _ storage.Interface = &FSTree{}
