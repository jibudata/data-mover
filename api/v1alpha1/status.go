package v1alpha1

import "time"

// Phases
const (
	PhaseCreated   = "Created"
	PhaseStarted   = "Started"
	PhaseCleanup   = "Cleanup"
	PhaseFailed    = "Failed"
	PhaseCompleted = "Completed"

	// export
	PhaseCreateTempNamespaceCreated = "CreateTempNamespace"
	PhaseCreateVolumeSnapshot       = "CreateVolumeSnapshot"
	PhaseUpdateSnapshotContent      = "UpdateSnapshotContent"
	PhaseCreatePVClaim              = "CreatePVClaim"
	PhaseRecreatePVClaim            = "RecreatePVClaim"
	PhaseCreateStagePod             = "CreateStagePod"
	PhaseWaitStagePodRunning        = "WaitStagePodRunning"
	PhaseStartFileSystemCopy        = "StartFileSystemCopy"
	PhaseWaitFileSystemCopyComplete = "WaitFileSystemCopyComplete"

	// import
	PhaseRetrieveFileSystemCopy = "RetrieveFileSystemCopy"
	PhaseDeleteOriginNamespace  = "DeleteOriginNamespace"
	PhaseRestoreTempNamespace   = "RestoreTempNamespace"
	PhaseDeleteStagePod         = "DeleteStagePod"
	PhaseRestoreOriginNamespace = "RestoreOriginNamespace"
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
