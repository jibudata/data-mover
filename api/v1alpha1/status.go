package v1alpha1

// Phases
const (
	PhaseCreated = ""
	PhaseStarted = "Started"
	// TBD
	PhaseFailed    = "Failed"
	PhaseCompleted = "Completed"
)

// Messages
const (
// TBD
)

func (r *VeleroExportStatus) SetReconcileFailed(err error) {
	// TBD
}

func (r *VeleroImportStatus) SetReconcileFailed(err error) {
	// TBD
}
