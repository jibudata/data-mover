package v1alpha1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Step
type Step struct {
	// A phase name.
	Phase string
}

// Phases
const (
	PhaseInitial             = ""
	PhaseCleanUp             = "CleanUp"
	PhaseCompleted           = "Completed"
	PhaseWaitCleanUpComplete = "WaitCleanUpComplete"

	// export
	PhaseCreated                    = ""
	PhasePrecheck                   = "Precheck"
	PhasePrepare                    = "Prepare"
	PhaseWaitPrepareComplete        = "WaitPrepareComplete"
	PhaseCreateTempNamespace        = "CreateTempNamespace"
	PhaseCreateVolumeSnapshot       = "CreateVolumeSnapshot"
	PhaseUpdateSnapshotContent      = "UpdateSnapshotContent"
	PhaseCheckSnapshotReady         = "CheckSnapshotReady"
	PhaseCreatePvc                  = "CreatePvc"
	PhaseCreatePvcPod               = "CreatePvcPod"
	PhaseWaitPvcPodRunning          = "WaitPvcPodRunning"
	PhaseCheckPvcReady              = "CheckPvcReady"
	PhaseCleanPvcPod                = "CleanPvcPod"
	PhaseEnsurePvcPodCleaned        = "EnsurePvcPodCleaned"
	PhaseUdpatePvClaimRetain        = "UdpatePvClaimRetain"
	PhaseDeletePvc                  = "DeletePvc"
	PhaseEnsurePvcDeleted           = "EnsurePvcDeleted"
	PhaseRecreatePvc                = "RecreatePvc"
	PhaseUpdatePvClaimRef           = "UpdatePvClaimRef"
	PhaseEnsureRecreatePvcReady     = "sEnsureRecreatePvcReady"
	PhaseUdpatePvClaimDelete        = "UdpatePvClaimDelete"
	PhaseCreateStagePod             = "CreateStagePod"
	PhaseWaitStagePodRunning        = "WaitStagePodRunning"
	PhaseStartFileSystemCopy        = "StartFileSystemCopy"
	PhaseUpdateSnapshotContentBack  = "UpdateSnapshotContentBack"
	PhaseCheckSnapshotContentBack   = "CheckSnapshotContentBack"
	PhaseWaitFileSystemCopyComplete = "WaitFileSystemCopyComplete"
	PhaseDeleteVolumeSnapshot       = "DeleteVolumeSnapshot"

	// import
	PhaseRetrieveFileSystemCopy = "RetrieveFileSystemCopy"
	//PhaseDeleteOriginNamespace  = "DeleteOriginNamespace" // Not needed in operator, restore missing volumnsnapshot
	PhaseRestoreTempNamespace     = "RestoreTempNamespace"
	PhaseRestoringTempNamespace   = "PhaseRestoringTempNamespace"
	PhaseDeleteStagePod           = "DeleteStagePod"
	PhaseDeletingStagePod         = "PhaseDeletingStagePod"
	PhaseRestoreOriginNamespace   = "RestoreOriginNamespace"
	PhaseRestoringOriginNamespace = "PhaseRestoringOriginNamespace"
)

// State
const (
	StateInProgress   = "InProgress"
	StateFailed       = "Failed"
	StateCompleted    = "Completed"
	StateVeleroFailed = "VeleroFailed"
	StateCanceled     = "Canceled"
)

//Requeue Time Definition
const (
	FastReQ     = time.Duration(time.Second * 20)
	PollReQ     = time.Duration(time.Second * 30)
	NoReQ       = time.Duration(time.Second * 10)
	LongPollReQ = time.Duration(time.Second * 100)
)

func GetNextPhase(phase string, allSteps []Step) string {
	current := -1
	for i, step := range allSteps {
		if step.Phase != phase {
			continue
		}
		current = i
		break
	}
	if current == -1 {
		return PhaseCompleted
	} else {
		current += 1
		return allSteps[current].Phase
	}
}

func GetLastPhase(allSteps []Step) string {
	return allSteps[len(allSteps)-1].Phase
}

func (r *VeleroExportStatus) UpdateStatus(phase string, message string) error {
	// TBD
	return nil
}

func (s *VeleroImportStatus) Update(phase string, state string, message string, restoreRef *corev1.ObjectReference) {
	if state == StateFailed || state == StateCompleted {
		s.CompletionTimestamp = &metav1.Time{Time: time.Now()}
		if state == StateFailed {
			s.Message = message
		}
	}
	if restoreRef != nil {
		s.VeleroRestoreRef = restoreRef
	}
	s.Phase = phase
	s.State = state
}

func (r *VeleroExportStatus) SetReconcileFailed(err error) {
	// TBD
}

func (r *VeleroImportStatus) SetReconcileFailed(err error) {
	// TBD
}
