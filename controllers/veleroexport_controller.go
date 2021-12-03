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

	"github.com/go-logr/logr"
	dmapi "github.com/jibudata/data-mover/api/v1alpha1"
	config "github.com/jibudata/data-mover/pkg/config"
	ops "github.com/jibudata/data-mover/pkg/operation"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// VeleroExportReconciler reconciles a VeleroExport object
type VeleroExportReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VeleroExport object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *VeleroExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var err error

	// Retrieve the VeleroExport object to be retrieved
	veleroExport := &dmapi.VeleroExport{}
	err = r.Get(ctx, req.NamespacedName, veleroExport)
	if err != nil {
		r.Log.Error(err, "")
		return ctrl.Result{Requeue: true}, nil
	}

	// Report reconcile error.
	defer func() {
		if err == nil || errors.IsConflict(err) {
			return
		}
		veleroExport.Status.SetReconcileFailed(err)
		err := r.Update(ctx, veleroExport)
		if err != nil {
			//r.Log.Error(err, "")
			return
		}
	}()
	backupName := veleroExport.Spec.VeleroBackupRef.Name
	tmpNs := "dm-" + backupName
	backupNs := veleroExport.Spec.BackupNamespace
	veleroNs := veleroExport.Spec.VeleroBackupRef.Namespace
	opt := ops.NewOperation(r.Log, r.Client, tmpNs)
	// precheck
	if veleroExport.Status.Phase == "" {
		veleroExport.Status = dmapi.VeleroExportStatus{}
		veleroExport.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
		err = r.Precheck(r.Client, veleroExport, opt)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		} else {
			r.Log.Info("snapshot export started")
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseStarted, err)
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreated || veleroExport.Status.Phase == dmapi.PhaseStarted {
		// do nothing
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseCreateTempNamespaceCreated, err)
	}
	if veleroExport.Status.Phase == dmapi.PhaseCreateTempNamespaceCreated {
		r.Log.Info("CreateNamespace()", "tmpNs", tmpNs)
		err = opt.CreateNamespace(tmpNs, true)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseCreateVolumeSnapshot, err)
	}
	vsList, err := opt.GetVolumeSnapshotList(backupName, backupNs)
	if err != nil {
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
		return ctrl.Result{}, err
	}
	var vsrl = make([]*ops.VolumeSnapshotResource, len(vsList.Items))
	if veleroExport.Status.Phase == dmapi.PhaseCreateVolumeSnapshot {
		r.Log.Info("CreateVolumeSnapshots()", "backupName", backupName, "backupNs", backupNs)
		vsrl, err = opt.CreateVolumeSnapshots(backupName, backupNs)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseUpdateSnapshotContent, err)
	}
	if veleroExport.Status.Phase == dmapi.PhaseUpdateSnapshotContent {
		r.Log.Info("SyncUpdateVolumeSnapshotContents()", "vsrl", vsrl)
		if vsrl[0] == nil {
			vsrl, err = opt.GetVolumeSnapshotResources(backupName, backupNs, tmpNs)
			if err != nil {
				r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
				return ctrl.Result{}, err
			}
		}
		err = opt.SyncUpdateVolumeSnapshotContents(vsrl)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseCreatePVClaim, err)

	}
	if veleroExport.Status.Phase == dmapi.PhaseCreatePVClaim {
		r.Log.Info("CreatePvcsWithVs()", "vsrl", vsrl, "backupNs", backupNs)
		if vsrl[0] == nil {
			vsrl, err = opt.GetVolumeSnapshotResources(backupName, backupNs, tmpNs)
			if err != nil {
				r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
				return ctrl.Result{}, err
			}
		}
		err = opt.CreatePvcsWithVs(vsrl, backupNs)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseRecreatePVClaim, err)
		return ctrl.Result{Requeue: true}, nil
	}
	if veleroExport.Status.Phase == dmapi.PhaseRecreatePVClaim {
		r.Log.Info("CreatePvcsWithPv()", "vsrl", vsrl, "backupNs", backupNs)
		if vsrl[0] == nil {
			vsrl, err = opt.GetVolumeSnapshotResources(backupName, backupNs, tmpNs)
			if err != nil {
				r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
				return ctrl.Result{}, err
			}
		}
		err = opt.CreatePvcsWithPv(vsrl, backupNs)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseCreateStagePod, err)
		return ctrl.Result{Requeue: true}, nil
	}
	if veleroExport.Status.Phase == dmapi.PhaseCreateStagePod {
		r.Log.Info("BuildStagePod()", "backupNs", backupNs)
		err = opt.BuildStagePod(backupNs, false)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		} else {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseWaitStagePodRunning, err)
			return ctrl.Result{Requeue: true}, nil
		}
	}
	if veleroExport.Status.Phase == dmapi.PhaseWaitStagePodRunning {
		r.Log.Info("GetStagePodStatus()")
		running := opt.GetStagePodStatus()
		if !running {
			return ctrl.Result{Requeue: true}, nil
		}
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseStartFileSystemCopy, err)
	}

	if veleroExport.Status.Phase == dmapi.PhaseStartFileSystemCopy {
		r.Log.Info("AsyncBackupNamespaceFc()", "backupName", backupName, "backupNs", veleroNs)
		backupPlan, err := opt.AsyncBackupNamespaceFc(backupName, veleroNs)
		if err != nil {
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
			return ctrl.Result{}, err
		}
		labels := map[string]string{
			config.SnapshotExportBackupName: backupPlan.Name,
		}
		veleroExport.Labels = labels
		err = r.Client.Update(ctx, veleroExport)
		if err != nil {
			r.Log.Error(err, "Failed to update labels")
			r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
		}
		// need to update label with
		r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseWaitFileSystemCopyComplete, err)
		return ctrl.Result{Requeue: true}, nil
	}
	if veleroExport.Status.Phase == dmapi.PhaseWaitFileSystemCopyComplete {
		bpName := veleroExport.Labels["snapshot-export-backup-name"]
		r.Log.Info("GetBackupStatus()", "backupPlan", veleroExport.Labels["snapshot-export-backup-name"])
		phase, err := opt.GetBackupStatus(bpName, veleroNs)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		} else {
			if phase == velero.BackupPhaseCompleted {
				r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseCompleted, err)
				return ctrl.Result{}, nil

			} else if phase == velero.BackupPhasePartiallyFailed || phase == velero.BackupPhaseFailed {
				r.updateStatusPhase(r.Client, veleroExport, dmapi.PhaseFailed, err)
				return ctrl.Result{}, err
			}
		}
	}
	if veleroExport.Status.Phase == dmapi.PhaseCompleted || veleroExport.Status.Phase == dmapi.PhaseFailed {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

func (r *VeleroExportReconciler) updateStatusPhase(client k8sclient.Client, veleroExport *dmapi.VeleroExport, phase string, err error) {
	if phase == dmapi.PhaseCompleted || phase == dmapi.PhaseFailed {
		veleroExport.Status.CompletionTimestamp = &metav1.Time{Time: time.Now()}
		if phase == dmapi.PhaseFailed {
			veleroExport.Status.Message = err.Error()
		}
	}
	veleroExport.Status.Phase = phase
	_ = r.Client.Status().Update(context.Background(), veleroExport)
	r.Log.Info("snapshot export status update", "phase", veleroExport.Status.Phase)
}

func (r *VeleroExportReconciler) Precheck(client k8sclient.Client, veleroExport *dmapi.VeleroExport, opt *ops.Operation) error {
	backupRef := veleroExport.Spec.VeleroBackupRef
	r.Log.Info("Precheck()", "backupRef.Name", backupRef.Name, "backupRef.Namespace", backupRef.Namespace)
	backup, err := opt.GetBackupPlan(backupRef.Name, backupRef.Namespace)
	if err != nil || backup.Status.Phase != velero.BackupPhaseCompleted || *backup.Spec.SnapshotVolumes != true {
		err = fmt.Errorf("illegal backup plan %s.", backupRef.Name)
	} else {
		r.Log.Info("Got velero backup plan", "backup plan", backupRef.Name, "in namespace", backupRef.Namespace, "for namespace", veleroExport.Spec.BackupNamespace)
		vsList, err := opt.GetVolumeSnapshotList(backupRef.Name, veleroExport.Spec.BackupNamespace)
		if err != nil {
			r.Log.Info("validate backup failed, could not get volume snapshot", "backup plan", backupRef.Name)
			err = fmt.Errorf("could not find volumesnapshot to export")
		} else {
			if len(vsList.Items) == 0 {
				err = fmt.Errorf("empty volumesnapshot list to export")
			}
		}
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *VeleroExportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&dmapi.VeleroExport{}).
		Named("VeleroExport").
		Build(r)
	if err != nil {
		return err
	}

	// Watch for changes to veleroexport
	err = c.Watch(
		&source.Kind{Type: &dmapi.VeleroExport{}},
		&handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}
