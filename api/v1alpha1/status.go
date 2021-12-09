package v1alpha1

import "time"

// Phases
const (
	// export
	PhasePrecheck                   = "Precheck"
	PhaseCreateTempNamespace        = "CreateTempNamespace"
	PhaseCreateVolumeSnapshot       = "CreateVolumeSnapshot"
	PhaseUpdateSnapshotContent      = "UpdateSnapshotContent"
	PhaseCreatePVClaim              = "CreatePVClaim"
	PhaseRecreatePVClaim            = "RecreatePVClaim"
	PhaseCreateStagePod             = "CreateStagePod"
	PhaseWaitStagePodRunning        = "WaitStagePodRunning"
	PhaseStartFileSystemCopy        = "StartFileSystemCopy"
	PhaseWaitFileSystemCopyComplete = "WaitFileSystemCopyComplete"
	PhaseCleanUp                    = "CleanUp"
	PhaseCompleted                  = "Completed"
	PhaseWaitCleanUpComplete        = "WaitCleanUpComplete"

	// import
	PhaseRetrieveFileSystemCopy = "RetrieveFileSystemCopy"
	PhaseDeleteOriginNamespace  = "DeleteOriginNamespace"
	PhaseRestoreTempNamespace   = "RestoreTempNamespace"
	PhaseDeleteStagePod         = "DeleteStagePod"
	PhaseRestoreOriginNamespace = "RestoreOriginNamespace"
)

// Step
type Step struct {
	// A phase name.
	Phase string
}

// State
const (
	StateInProgress = "InProgress"
	StateFailed     = "Failed"
	StateCompleted  = "Completed"
)

// Messages
const (
	FastReQ     = time.Duration(time.Second * 20)
	PollReQ     = time.Duration(time.Second * 30)
	NoReQ       = time.Duration(time.Second * 10)
	LongPollReQ = time.Duration(time.Second * 100)
)

func (r *VeleroExportStatus) UpdateStatus(phase string, message string) error {
	// TBD
	return nil
}

func (r *VeleroExportStatus) SetReconcileFailed(err error) {
	// TBD
}

func (r *VeleroImportStatus) SetReconcileFailed(err error) {
	// TBD
}
