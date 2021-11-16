package v1alpha1

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
	PhaseStartFileSystemCopy        = "StartFileSystemCopy"

	// import
	PhaseRetrieveFileSystemCopy = "RetrieveFileSystemCopy"
	PhaseDeleteOriginNamespace  = "DeleteOriginNamespace"
	PhaseRestoreTempNamespace   = "RestoreTempNamespace"
	PhaseDeleteStagePod         = "DeleteStagePod"
	PhaseRestoreOriginNamespace = "RestoreOriginNamespace"
)

// Messages
const (
// TBD
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
