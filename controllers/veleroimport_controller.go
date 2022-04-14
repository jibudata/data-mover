/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	dmapi "github.com/jibudata/data-mover/api/v1alpha1"
	config "github.com/jibudata/data-mover/pkg/config"
	operation "github.com/jibudata/data-mover/pkg/operation"
)

// VeleroImportReconciler reconciles a VeleroImport object
type VeleroImportReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Steps  []dmapi.Step
}

const (
	// Info Message

	// Failure Message
	MessageObjectNotFound = "Object Not Found"
)

var veleroImportSteps = []dmapi.Step{
	{Phase: dmapi.PhaseInitial},
	{Phase: dmapi.PhaseRestoreTempNamespace},
	{Phase: dmapi.PhaseRestoringTempNamespace},
	{Phase: dmapi.PhaseDeleteStagePod},
	{Phase: dmapi.PhaseDeletingStagePod},
	{Phase: dmapi.PhaseRestoreOriginNamespace},
	{Phase: dmapi.PhaseRestoringOriginNamespace},
	// {Phase: dmapi.PhaseCleanUp},
	// {Phase: dmapi.PhaseWaitCleanUpComplete},
	{Phase: dmapi.PhaseCompleted},
}

var veleroImportSnapshotSteps = []dmapi.Step{
	{Phase: dmapi.PhaseInitial},
	{Phase: dmapi.PhaseRestoreTempNamespace},
	{Phase: dmapi.PhaseRestoringTempNamespace},
	{Phase: dmapi.PhaseDeleteStagePod},
	{Phase: dmapi.PhaseDeletingStagePod},
	// {Phase: dmapi.PhaseCleanUp},
	// {Phase: dmapi.PhaseWaitCleanUpComplete},
	{Phase: dmapi.PhaseCompleted},
}
var steps []dmapi.Step

func (r *VeleroImportReconciler) nextPhase(phase string) string {
	return dmapi.GetNextPhase(phase, steps)
}

func getRestoreObjectReference(restore *velero.Restore) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Kind:      "Restore",
		Name:      restore.GetName(),
		Namespace: restore.GetNamespace(),
		UID:       restore.GetUID(),
	}
}

func (r *VeleroImportReconciler) UpdateStatus(ctx context.Context, veleroImport *dmapi.VeleroImport, restore *velero.Restore, err error) error {
	logger := log.FromContext(ctx)
	if restore != nil {
		veleroImport.Status.VeleroRestoreRef = getRestoreObjectReference(restore)
	}
	if err != nil {
		veleroImport.Status.Message = err.Error()
		if veleroImport.Status.State != dmapi.StateVeleroFailed {
			veleroImport.Status.State = dmapi.StateFailed
		}
		if veleroImport.Status.LastFailureTimestamp == nil {
			veleroImport.Status.LastFailureTimestamp = &metav1.Time{Time: time.Now()}
		}
		logger.Error(err, "snapshot import failure", "phase", veleroImport.Status.Phase)
	} else {
		if veleroImport.Status.LastFailureTimestamp != nil {
			veleroImport.Status.LastFailureTimestamp = nil
		}
		veleroImport.Status.Message = ""
		veleroImport.Status.Phase = r.nextPhase(veleroImport.Status.Phase)
		if veleroImport.Status.Phase == dmapi.GetLastPhase(steps) {
			veleroImport.Status.State = dmapi.StateCompleted
			veleroImport.Status.CompletionTimestamp = &metav1.Time{Time: time.Now()}
		} else {
			veleroImport.Status.State = dmapi.StateInProgress
		}
	}
	logger.Info("snapshot import status update", "phase", veleroImport.Status.Phase, "state", veleroImport.Status.State)
	err = r.Client.Status().Update(context.TODO(), veleroImport)
	return err
}

//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroimports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroimports/finalizers,verbs=update
//+kubebuilder:rbac:groups=velero.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VeleroImport object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *VeleroImportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var err error

	// Retrieve the VeleroExport object to be retrieved
	veleroImport := &dmapi.VeleroImport{}
	err = r.Get(ctx, req.NamespacedName, veleroImport)
	if err != nil {
		logger.Error(err, MessageObjectNotFound)
		return ctrl.Result{Requeue: true}, nil
	}
	if veleroImport.Spec.SnapshotOnly {
		steps = veleroImportSnapshotSteps
	} else {
		steps = veleroImportSteps
	}

	// Report reconcile error.
	defer func() {
		if err == nil || errors.IsConflict(err) {
			return
		}
		veleroImport.Status.SetReconcileFailed(err)
		err := r.Update(ctx, veleroImport)
		if err != nil {
			logger.Error(err, "")
			return
		}
	}()

	backupName := veleroImport.Spec.VeleroBackupRef.Name
	veleroNamespace := veleroImport.Spec.VeleroBackupRef.Namespace
	// restoreNamespace := veleroImport.Spec.RestoreNamespace
	namespaceMapping := veleroImport.Spec.NamespaceMapping
	handler := operation.NewOperation(r.Log, r.Client)

	if veleroImport.Status.Phase == dmapi.PhaseCompleted ||
		veleroImport.Status.State == dmapi.StateVeleroFailed ||
		veleroImport.Status.State == dmapi.StateCanceled {

		logger.Info("Restore " + veleroImport.Status.State)
		return ctrl.Result{}, nil
	}

	if veleroImport.Status.Phase == dmapi.PhaseInitial {
		veleroImport.Status = dmapi.VeleroImportStatus{}
		veleroImport.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
		logger.Info("Snapshot Import Started")
		err = r.UpdateStatus(ctx, veleroImport, nil, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroImport.Status.State == dmapi.StateFailed {
		if veleroImport.Status.LastFailureTimestamp != nil {
			if time.Since(veleroImport.Status.LastFailureTimestamp.Time) >= timeout {
				logger.Info("Failed veleroImport got timeout", "veleroImport", veleroImport.Name)
				veleroImport.Status.State = dmapi.StateCanceled
				err = r.Client.Status().Update(context.TODO(), veleroImport)
				if err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
			}
		}
	}

	err = r.Validate(ctx, veleroImport, handler)
	if err != nil {
		logger.Info("Validate failure", "velero import name", veleroImport.Name, "error", err)
		r.UpdateStatus(ctx, veleroImport, nil, err)
		return ctrl.Result{}, err
	}

	if veleroImport.Status.Phase == dmapi.PhaseRestoreTempNamespace {
		logger.Info("Start invoking velero to restore the temporary namespace to given namespace ...")
		backup, err := handler.GetVeleroBackup(backupName, veleroNamespace)
		if err != nil || backup == nil {
			logger.Error(err, fmt.Sprintf("Failed to get velero backup %s: %s", backupName, err.Error()))
			r.UpdateStatus(ctx, veleroImport, nil, err)
			return ctrl.Result{}, err
		}
		fcNamespaceMapping := make(map[string]string)
		for srcNamespace, tgtNamespace := range namespaceMapping {
			fcNamespaceMapping[config.TempNamespacePrefix+srcNamespace] = tgtNamespace
		}

		suffix := handler.GetRestoreJobSuffix(veleroImport)
		rateLimit := ""
		if veleroImport.Spec.RateLimitValue > 0 {
			rateLimit = fmt.Sprintf("%d", veleroImport.Spec.RateLimitValue)
		}
		restore, err := handler.EnsureVeleroRestore(veleroImport, backup.Name, config.VeleroNamespace, suffix, rateLimit, fcNamespaceMapping, false)
		if err != nil {
			r.UpdateStatus(ctx, veleroImport, nil, err)
			return ctrl.Result{}, err
		}

		r.UpdateStatus(ctx, veleroImport, restore, nil)
		return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
	}

	if veleroImport.Status.Phase == dmapi.PhaseRestoringTempNamespace {
		logger.Info("Check original namespace restore status ...")
		restore := handler.GetVeleroRestore(veleroImport.Status.VeleroRestoreRef.Name, config.VeleroNamespace)
		if restore == nil {
			r.UpdateStatus(ctx, veleroImport, nil, fmt.Errorf("failed to get velero restore"))
			return ctrl.Result{}, err
		} else {
			if restore.Status.Phase == velero.RestorePhaseCompleted {
				r.UpdateStatus(ctx, veleroImport, restore, nil)
			} else if restore.Status.Phase == velero.RestorePhaseFailed ||
				restore.Status.Phase == velero.RestorePhaseFailedValidation ||
				restore.Status.Phase == velero.RestorePhasePartiallyFailed {
				err = fmt.Errorf("velero backup failed")
				veleroImport.Status.State = dmapi.StateVeleroFailed
				r.UpdateStatus(ctx, veleroImport, restore, err)
				return ctrl.Result{}, err
			} else {
				return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
			}
		}
	}

	if veleroImport.Status.Phase == dmapi.PhaseDeleteStagePod {
		logger.Info("Start delete pod in given namespace ...")
		for _, tgtNamespace := range namespaceMapping {
			err = handler.EnsureStagePodDeleted(tgtNamespace)
			if err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete pod in given namespace %s: %s", tgtNamespace, err.Error()))
				r.UpdateStatus(ctx, veleroImport, nil, err)
				return ctrl.Result{}, err
			}
		}

		r.UpdateStatus(ctx, veleroImport, nil, nil)
		return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
	}

	if veleroImport.Status.Phase == dmapi.PhaseDeletingStagePod {
		logger.Info("Check pod deletion status ...")
		for _, tgtNamespace := range namespaceMapping {
			deleted := handler.IsStagePodDeleted(tgtNamespace)
			if !deleted {
				r.UpdateStatus(ctx, veleroImport, nil, fmt.Errorf("stage pod still exists"))
				return ctrl.Result{RequeueAfter: requeueAfterFast}, err
			} else {
				err = r.UpdateStatus(ctx, veleroImport, nil, nil)
				if err != nil {
					return ctrl.Result{Requeue: true}, nil
				}
			}
		}
	}

	if veleroImport.Status.Phase == dmapi.PhaseRestoreOriginNamespace {
		logger.Info("Start invoking velero to restore original namespace ...")
		suffix := "orig-" + handler.GetRestoreJobSuffix(veleroImport)
		restore, err := handler.EnsureVeleroRestore(veleroImport, backupName, config.VeleroNamespace, suffix, "", namespaceMapping, false)
		if err != nil {
			logger.Error(err, fmt.Sprint("Failed to restore original namespace", err.Error()))
			r.UpdateStatus(ctx, veleroImport, nil, err)
			return ctrl.Result{}, err
		}
		r.UpdateStatus(ctx, veleroImport, restore, nil)
		return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
	}

	if veleroImport.Status.Phase == dmapi.PhaseRestoringOriginNamespace {
		logger.Info("Check original namespace restore status ...")
		restored := handler.IsNamespaceRestored(veleroImport.Status.VeleroRestoreRef.Name, config.VeleroNamespace)
		if !restored {
			return ctrl.Result{RequeueAfter: requeueAfterSlow}, err
		} else {
			restore := handler.GetVeleroRestore(veleroImport.Status.VeleroRestoreRef.Name, config.VeleroNamespace)
			err = r.UpdateStatus(ctx, veleroImport, restore, nil)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *VeleroImportReconciler) Validate(ctx context.Context, veleroImport *dmapi.VeleroImport, handler *operation.Operation) error {
	// Check veleroBackupRef existance
	backupRef := veleroImport.Spec.VeleroBackupRef
	if !handler.RefSet(backupRef) {
		err := fmt.Errorf("invalid velero backup reference %s", veleroImport.Name)
		return err
	}
	// logger.Info("Validate()", "backupRef.Name", backupRef.Name, "backupRef.Namespace", backupRef.Namespace)
	backup, err := handler.GetBackupPlan(backupRef.Name, backupRef.Namespace)
	if err != nil || backup.Status.Phase != velero.BackupPhaseCompleted || !*backup.Spec.SnapshotVolumes {
		err = fmt.Errorf("invalid backup plan %s", backupRef.Name)
		return err
	}
	importRef := veleroImport.Spec.DataImportRef
	if !handler.RefSet(importRef) {
		err = fmt.Errorf("invalid data import reference %s", veleroImport.Name)
		return err
	}

	// TBD: If restore namespace already exist
	// TBD: If required volumnsnapshots already exists under namespace
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VeleroImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&dmapi.VeleroImport{}).
		Named("VeleroImport").
		Build(r)
	if err != nil {
		return err
	}

	// Watch for changes to veleroimport
	err = c.Watch(
		&source.Kind{Type: &dmapi.VeleroImport{}},
		&handler.EnqueueRequestForObject{})
	return err
}
