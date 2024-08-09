// Package renameio provides a way to atomically create or replace a file or
// symbolic link.
//
// Caveat: this package requires the file system rename(2) implementation to be
// atomic. Notably, this is not the case when using NFS with multiple clients:
// https://stackoverflow.com/a/41396801
package renameio
