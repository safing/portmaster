package updater

import (
	"sort"
	"sync"
	"time"

	"github.com/safing/portmaster/base/utils"
)

// Registry States.
const (
	StateReady       = "ready"       // Default idle state.
	StateChecking    = "checking"    // Downloading indexes.
	StateDownloading = "downloading" // Downloading updates.
	StateFetching    = "fetching"    // Fetching a single file.
)

// RegistryState describes the registry state.
type RegistryState struct {
	sync.Mutex
	reg *ResourceRegistry

	// ID holds the ID of the state the registry is currently in.
	ID string

	// Details holds further information about the current state.
	Details any

	// Updates holds generic information about the current status of pending
	// and recently downloaded updates.
	Updates UpdateState

	// operationLock locks the operation of any state changing operation.
	// This is separate from the registry lock, which locks access to the
	// registry struct.
	operationLock sync.Mutex
}

// StateDownloadingDetails holds details of the downloading state.
type StateDownloadingDetails struct {
	// Resources holds the resource IDs that are being downloaded.
	Resources []string

	// FinishedUpTo holds the index of Resources that is currently being
	// downloaded. Previous resources have finished downloading.
	FinishedUpTo int
}

// UpdateState holds generic information about the current status of pending
// and recently downloaded updates.
type UpdateState struct {
	// LastCheckAt holds the time of the last update check.
	LastCheckAt *time.Time
	// LastCheckError holds the error of the last check.
	LastCheckError error
	// PendingDownload holds the resources that are pending download.
	PendingDownload []string

	// LastDownloadAt holds the time when resources were downloaded the last time.
	LastDownloadAt *time.Time
	// LastDownloadError holds the error of the last download.
	LastDownloadError error
	// LastDownload holds the resources that we downloaded the last time updates
	// were downloaded.
	LastDownload []string

	// LastSuccessAt holds the time of the last successful update (check).
	LastSuccessAt *time.Time
}

// GetState returns the current registry state.
// The returned data must not be modified.
func (reg *ResourceRegistry) GetState() RegistryState {
	reg.state.Lock()
	defer reg.state.Unlock()

	return RegistryState{
		ID:      reg.state.ID,
		Details: reg.state.Details,
		Updates: reg.state.Updates,
	}
}

// StartOperation starts an operation.
func (s *RegistryState) StartOperation(id string) bool {
	defer s.notify()

	s.operationLock.Lock()

	s.Lock()
	defer s.Unlock()

	s.ID = id
	return true
}

// UpdateOperationDetails updates the details of an operation.
// The supplied struct should be a copy and must not be changed after calling
// this function.
func (s *RegistryState) UpdateOperationDetails(details any) {
	defer s.notify()

	s.Lock()
	defer s.Unlock()

	s.Details = details
}

// EndOperation ends an operation.
func (s *RegistryState) EndOperation() {
	defer s.notify()
	defer s.operationLock.Unlock()

	s.Lock()
	defer s.Unlock()

	s.ID = StateReady
	s.Details = nil
}

// ReportUpdateCheck reports an update check to the registry state.
func (s *RegistryState) ReportUpdateCheck(pendingDownload []string, failed error) {
	defer s.notify()

	sort.Strings(pendingDownload)

	s.Lock()
	defer s.Unlock()

	now := time.Now()
	s.Updates.LastCheckAt = &now
	s.Updates.LastCheckError = failed
	s.Updates.PendingDownload = pendingDownload

	if failed == nil {
		s.Updates.LastSuccessAt = &now
	}
}

// ReportDownloads reports downloaded updates to the registry state.
func (s *RegistryState) ReportDownloads(downloaded []string, failed error) {
	defer s.notify()

	sort.Strings(downloaded)

	s.Lock()
	defer s.Unlock()

	now := time.Now()
	s.Updates.LastDownloadAt = &now
	s.Updates.LastDownloadError = failed
	s.Updates.LastDownload = downloaded

	// Remove downloaded resources from the pending list.
	if len(s.Updates.PendingDownload) > 0 {
		newPendingDownload := make([]string, 0, len(s.Updates.PendingDownload))
		for _, pending := range s.Updates.PendingDownload {
			if !utils.StringInSlice(downloaded, pending) {
				newPendingDownload = append(newPendingDownload, pending)
			}
		}
		s.Updates.PendingDownload = newPendingDownload
	}

	if failed == nil {
		s.Updates.LastSuccessAt = &now
	}
}

func (s *RegistryState) notify() {
	switch {
	case s.reg == nil:
		return
	case s.reg.StateNotifyFunc == nil:
		return
	}

	s.reg.StateNotifyFunc(s)
}
