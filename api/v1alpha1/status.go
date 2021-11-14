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

func (r *DataExportStatus) SetReconcileFailed(err error) {
	// TBD
}

func (r *DataImportStatus) SetReconcileFailed(err error) {
	// TBD
}
