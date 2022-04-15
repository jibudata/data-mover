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
	"strings"
	"time"

	"github.com/go-logr/logr"
	dmapi "github.com/jibudata/data-mover/api/v1alpha1"
	config "github.com/jibudata/data-mover/pkg/config"
	ops "github.com/jibudata/data-mover/pkg/operation"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
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

const (
	requeueAfterFast = 5 * time.Second
	requeueAfterSlow = 20 * time.Second
	timeout          = 15 * time.Minute
)

const (
	VolumeSnapshotResourceAnnPrefix = "Volume-Snapshot-Resource-"
)

var veleroExportSteps = []dmapi.Step{
	{Phase: dmapi.PhaseCreated, SkipClean: true},
	{Phase: dmapi.PhasePrecheck, SkipClean: true},
	{Phase: dmapi.PhasePrepare, SkipClean: true},
	{Phase: dmapi.PhaseWaitPrepareComplete},
	{Phase: dmapi.PhaseCreateTempNamespace},
	{Phase: dmapi.PhaseCreateVolumeSnapshot},
	{Phase: dmapi.PhaseUpdateSnapshotContent},
	{Phase: dmapi.PhaseCheckSnapshotReady},
	{Phase: dmapi.PhaseCreatePvc},
	{Phase: dmapi.PhaseCreatePvcPod},
	{Phase: dmapi.PhaseWaitPvcPodRunning},
	{Phase: dmapi.PhaseCheckPvcReady},
	{Phase: dmapi.PhaseCleanPvcPod},
	{Phase: dmapi.PhaseEnsurePvcPodCleaned},
	{Phase: dmapi.PhaseUpdatePvClaimRetain},
	{Phase: dmapi.PhaseDeletePvc},
	{Phase: dmapi.PhaseEnsurePvcDeleted},
	{Phase: dmapi.PhaseRecreatePvc},
	{Phase: dmapi.PhaseUpdatePvClaimRef},
	{Phase: dmapi.PhaseEnsureRecreatePvcReady},
	{Phase: dmapi.PhaseUpdatePvClaimDelete},
	{Phase: dmapi.PhaseUpdateSnapshotContentBack},
	// {Phase: dmapi.PhaseCheckSnapshotContentBack},
	{Phase: dmapi.PhaseDeleteVolumeSnapshot},
	{Phase: dmapi.PhaseCreateStagePod},
	{Phase: dmapi.PhaseWaitStagePodRunning},
	{Phase: dmapi.PhaseStartFileSystemCopy},
	{Phase: dmapi.PhaseWaitFileSystemCopyComplete},
	{Phase: dmapi.PhaseCleanUp},
	// {Phase: dmapi.PhaseWaitCleanUpComplete},
	{Phase: dmapi.PhaseCompleted},
}

func (r *VeleroExportReconciler) nextPhase(phase string) string {
	return dmapi.GetNextPhase(phase, veleroExportSteps)
}

//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=veleroexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=velero.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete

func (r *VeleroExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var err error

	// Retrieve the VeleroExport object to be retrieved
	veleroExport := &dmapi.VeleroExport{}
	err = r.Get(ctx, req.NamespacedName, veleroExport)
	if err != nil {
		logger.Info("Failed to get veleroExport CR", "error", err)
		return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
	}

	// Report reconcile error.
	defer func() {
		if err == nil || errors.IsConflict(err) {
			return
		}
		veleroExport.Status.SetReconcileFailed(err)
		err := r.Client.Status().Update(ctx, veleroExport)
		if err != nil {
			//logger.Error(err, "")
			return
		}
	}()

	backupName := veleroExport.Spec.VeleroBackupRef.Name
	includedNamespaces := veleroExport.Spec.IncludedNamespaces
	veleroNamespace := veleroExport.Spec.VeleroBackupRef.Namespace
	opt := ops.NewOperation(logger, r.Client)

	if veleroExport.Status.State == dmapi.StateFailed {
		if veleroExport.Status.LastFailureTimestamp != nil {
			if time.Since(veleroExport.Status.LastFailureTimestamp.Time) >= timeout {

				logger.Info("Failed veleroexport got timeout", "veleroexport", veleroExport.Name)
				err = r.cleanUp(opt, includedNamespaces, true)
				if err != nil {
					return ctrl.Result{}, err
				}

				veleroExport.Status.State = dmapi.StateCanceled
				err = r.Client.Status().Update(context.TODO(), veleroExport)
				if err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
			}
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCompleted ||
		veleroExport.Status.State == dmapi.StateCanceled ||
		veleroExport.Status.State == dmapi.StateVeleroFailed {
		if veleroExport.Status.StopTimestamp != nil {
			stopTime := veleroExport.Status.StopTimestamp.Time
			now := time.Now()
			if now.After(stopTime) {
				logger.Info("velero export expired", "now", now)
				err = r.deleteVeleroExport(veleroExport)
				if err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			} else {
				duration := stopTime.Sub(now)
				return ctrl.Result{RequeueAfter: duration}, nil
			}
		} else {
			return ctrl.Result{}, nil
		}
	}
	_ = r.validatePolicy(veleroExport)

	// check retention
	if veleroExport.Spec.Policy.Retention == time.Duration(0) {
		veleroExport.Spec.Policy.Retention = time.Duration(24)
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreated {
		onging, err := opt.CheckOngoingExport(veleroExport)
		if err != nil {
			return ctrl.Result{}, err
		}
		if onging {
			logger.Info("There are ongoing velero exports in cluster")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		logger.Info(
			"snapshot export started",
			"retention",
			int32(veleroExport.Spec.Policy.Retention),
		)
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// precheck
	if veleroExport.Status.Phase == dmapi.PhasePrecheck {

		err = r.precheck(r.Client, veleroExport, opt)
		r.updateStatus(ctx, r.Client, veleroExport, err)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// delete the temparory namespace already exists
	if veleroExport.Status.Phase == dmapi.PhasePrepare {
		logger.Info("[phase]: PhasePrepare")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace + backupName[strings.LastIndex(backupName, "-"):]
			err = opt.AsyncDeleteNamespace(tmpNamespace)
			if err != nil && errors.IsConflict(err) {
				// do nothing
				return ctrl.Result{Requeue: true}, nil
			} else if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseWaitPrepareComplete {
		logger.Info("[phase]: PhaseWaitPrepareComplete")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			_, err := opt.GetNamespace(tmpNamespace)
			if err != nil && errors.IsNotFound(err) {
				continue
			} else if err != nil {
				err = r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			} else {
				return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// create temp namespaces
	if veleroExport.Status.Phase == dmapi.PhaseCreateTempNamespace {

		logger.Info("[phase]: PhaseCreateTempNamespace")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			err = opt.CreateNamespace(tmpNamespace, false)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreateVolumeSnapshot {

		logger.Info("[phase]: PhaseCreateVolumeSnapshot")
		for _, namespace := range includedNamespaces {
			// get type VolumeSnapshotResource struct {
			//     VolumeSnapshotName string
			//     OrigVolumeSnapshotUID  types.UID
			//     PersistentVolumeClaimName string
			//     VolumeSnapshotContentName string
			//     VolumeSnapshotContentName string
			//     NewVoluemSnapshotUID types.UID
			//     PersistentVolumeName string
			// }
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				continue
			}

			tmpNamespace := config.TempNamespacePrefix + namespace
			vsrl, err := opt.CreateVolumeSnapshots(backupName, namespace, tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
			if len(vsrl) == 0 {
				continue
			}
			r.UpdateVsrlAnnotations(vsrl, namespace, veleroExport)
		}
		err = r.Update(ctx, veleroExport)
		if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		}

		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseUpdateSnapshotContent {

		logger.Info("[phase]: PhaseUpdateSnapshotContent")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.AsyncUpdateVolumeSnapshotContents(vsrl, tmpNamespace, false)
				if err != nil && errors.IsConflict(err) {
					// do nothing
					return ctrl.Result{Requeue: true}, nil
				} else if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, err)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCheckSnapshotReady {

		logger.Info("[phase]: PhaseCheckSnapshotReady")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				ready, err := opt.IsVolumeSnapshotReady(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
				if !ready {
					return ctrl.Result{RequeueAfter: requeueAfterFast}, err
				}
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreatePvc {

		logger.Info("[phase]: PhaseCreatePvc")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.CreatePvcsWithVs(vsrl, namespace, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreatePvcPod {

		logger.Info("[phase]: PhaseCreatePvcPod")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			err = opt.BuildStagePod(namespace, false, tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
		}

		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
		}
		return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseWaitPvcPodRunning {

		logger.Info("[phase]: PhaseWaitPvcPodRunning")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			running, err := opt.GetStagePodStatus(tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
			if !running {
				err = fmt.Errorf("pod state is not running")
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{}, err
		}

	}

	if veleroExport.Status.Phase == dmapi.PhaseCheckPvcReady {
		logger.Info("[phase]: PhaseCheckPvcReady")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				ready, err := opt.CheckPVCReady(tmpNamespace, vsrl)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
				if !ready {
					r.updateStatus(ctx, r.Client, veleroExport, fmt.Errorf("pvc not ready, namespace %s, will retry", tmpNamespace))
					return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
				}
				r.UpdateVsrlAnnotations(vsrl, namespace, veleroExport)
			}
		}
		err = r.Update(ctx, veleroExport)
		if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
		}

		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseCleanPvcPod {
		logger.Info("[phase]: PhaseCleanPvcPod")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			err := opt.EnsureStagePodDeleted(tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseEnsurePvcPodCleaned {

		logger.Info("[phase]: PhaseEnsurePvcPodCleaned")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			clean, err := opt.EnsureStagePodCleaned(tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
			if !clean {
				err = fmt.Errorf("stage pod still running")
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseUpdatePvClaimRetain {

		logger.Info("[phase]: PhaseUpdatePvClaimRetain")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.UpdatePvClaimRetain(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseDeletePvc {

		logger.Info("[phase]: PhaseDeletePvc")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.DeletePvc(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseEnsurePvcDeleted {

		logger.Info("[phase]: PhaseEnsurePvcDeleted")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				deleted, err := opt.EnsurePvcDeleted(tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{RequeueAfter: requeueAfterFast}, err
				}
				if !deleted {
					r.updateStatus(ctx, r.Client, veleroExport, fmt.Errorf("pvc still exists"))
					return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseRecreatePvc {

		logger.Info("[phase]: PhaseRecreatePvc")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.CreatePvcsWithPv(vsrl, namespace, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseUpdatePvClaimRef {

		logger.Info("[phase]: PhaseUpdatePvClaimRef")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.UpdatePvClaimRef(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseEnsureRecreatePvcReady {
		logger.Info("[phase]: PhaseEnsureRecreatePvcReady")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				ready, err := opt.CheckPVCReady(tmpNamespace, vsrl)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
				if !ready {
					r.updateStatus(ctx, r.Client, veleroExport, fmt.Errorf("pvc not ready, namespace %s, will retry", tmpNamespace))
					return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil

	}

	if veleroExport.Status.Phase == dmapi.PhaseUpdatePvClaimDelete {

		logger.Info("[phase]: PhaseUpdatePvClaimDelete")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.UpdatePvClaimDelete(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseUpdateSnapshotContentBack {

		logger.Info("[phase]: PhaseUpdateSnapshotContentBack")
		for _, namespace := range includedNamespaces {
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				err = opt.AsyncUpdateVolumeSnapshotContents(vsrl, namespace, true)
				if err != nil && errors.IsConflict(err) {
					// do nothing
					return ctrl.Result{Requeue: true}, nil
				} else if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{RequeueAfter: requeueAfterFast}, err
				}
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCheckSnapshotContentBack {

		logger.Info("[phase]: PhaseCheckSnapshotContentBack")
		for _, namespace := range includedNamespaces {
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				ready, err := opt.IsVolumeSnapshotReady(vsrl, namespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{}, err
				}
				if !ready {
					r.updateStatus(ctx, r.Client, veleroExport, fmt.Errorf("snapshot not ready"))
					return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
				}
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseDeleteVolumeSnapshot {

		logger.Info("[phase]: PhaseDeleteVolumeSnapshot")
		for _, namespace := range includedNamespaces {
			if veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] != "" {
				vsrl := opt.GetVolumeSnapshotResourceList(veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace])
				if vsrl == nil {
					continue
				}
				tmpNamespace := config.TempNamespacePrefix + namespace
				err = opt.DeleteVolumeSnapshots(vsrl, tmpNamespace)
				if err != nil {
					r.updateStatus(ctx, r.Client, veleroExport, err)
					return ctrl.Result{RequeueAfter: requeueAfterFast}, err
				}
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseCreateStagePod {

		logger.Info("[phase]: PhaseCreateStagePod")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			err = opt.BuildStagePod(namespace, false, tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
		}
		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseWaitStagePodRunning {

		logger.Info("[phase]: PhaseWaitStagePodRunning")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			running, err := opt.GetStagePodStatus(tmpNamespace)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}
			if !running {
				err = fmt.Errorf("pod state is not running")
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
			}
		}
		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if veleroExport.Status.Phase == dmapi.PhaseStartFileSystemCopy {

		logger.Info("[phase]: PhaseStartFileSystemCopy")
		bpName := veleroExport.Labels[config.SnapshotExportBackupName]
		veleroPlan, err := opt.GetVeleroBackup(bpName, veleroNamespace)
		if (err != nil && errors.IsNotFound(err)) || (veleroPlan == nil && err == nil) {
			logger.Info("velero plan doesn't exist")
			var backupNamespaces []string
			for _, namespace := range includedNamespaces {
				tmpNamespace := config.TempNamespacePrefix + namespace
				backupNamespaces = append(backupNamespaces, tmpNamespace)
			}

			rateLimit := ""
			if veleroExport.Spec.RateLimitValue > 0 {
				rateLimit = fmt.Sprintf("%d", veleroExport.Spec.RateLimitValue)
			}
			veleroPlan, err = opt.EnsureVeleroBackup(backupName, veleroNamespace, rateLimit, backupNamespaces)
			if err != nil {
				r.updateStatus(ctx, r.Client, veleroExport, err)
				return ctrl.Result{}, err
			}

		} else if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		}
		err = r.updateVeleroExportLabel(r.Client, veleroExport, veleroPlan)
		if err != nil && errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		}

		err = r.updateStatus(ctx, r.Client, veleroExport, nil)
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{RequeueAfter: requeueAfterSlow}, err
	}

	if veleroExport.Status.Phase == dmapi.PhaseWaitFileSystemCopyComplete {

		logger.Info("[phase]: PhaseWaitFileSystemCopyComplete")
		bpName := veleroExport.Labels[config.SnapshotExportBackupName]
		bpPhase, err := opt.GetBackupStatus(bpName, veleroNamespace)
		if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		}

		if bpPhase == velero.BackupPhaseCompleted {
			r.updateStatus(ctx, r.Client, veleroExport, nil)
		} else if bpPhase == velero.BackupPhasePartiallyFailed ||
			bpPhase == velero.BackupPhaseFailed ||
			bpPhase == velero.BackupPhaseFailedValidation {

			logger.Error(fmt.Errorf("velero backup failed"), "clean up resources")
			err = r.cleanUp(opt, includedNamespaces, true)
			if err != nil {
				return ctrl.Result{}, err
			}

			veleroExport.Status.State = dmapi.StateVeleroFailed
			err = fmt.Errorf("velero backup failed")
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		} else {
			return ctrl.Result{RequeueAfter: requeueAfterSlow}, nil
		}

	}

	if veleroExport.Status.Phase == dmapi.PhaseCleanUp {

		logger.Info("[phase]: PhaseCleanUp")
		err = r.cleanUp(opt, includedNamespaces, false)
		if err != nil {
			r.updateStatus(ctx, r.Client, veleroExport, err)
			return ctrl.Result{}, err
		}

		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{RequeueAfter: requeueAfterFast}, nil
	}

	if veleroExport.Status.Phase == dmapi.PhaseWaitCleanUpComplete {

		logger.Info("[phase]: PhaseWaitCleanUpComplete")
		for _, namespace := range includedNamespaces {
			tmpNamespace := config.TempNamespacePrefix + namespace
			_, err = opt.GetNamespace(tmpNamespace)
			if err != nil && errors.IsNotFound(err) {
				continue
			} else {
				return ctrl.Result{Requeue: true}, nil
			}
		}

		r.updateStatus(ctx, r.Client, veleroExport, nil)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VeleroExportReconciler) cleanUp(opt *ops.Operation, includedNamespaces []string, deletePv bool) error {
	var err error
	// clean up tempary namespaces
	tmpNamespaces := []string{}
	for _, namespace := range includedNamespaces {
		tmpNamespace := config.TempNamespacePrefix + namespace
		tmpNamespaces = append(tmpNamespaces, tmpNamespace)
		err = opt.AsyncDeleteNamespace(tmpNamespace)
		if err != nil {
			return err
		}
	}
	if deletePv {
		// clean up pvs
		err = opt.ClearPVs(tmpNamespaces)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *VeleroExportReconciler) updateVeleroExportLabel(client k8sclient.Client, veleroExport *dmapi.VeleroExport, veleroPlan *velero.Backup) error {

	// updatre backup plan in velero export labels
	if veleroExport.Labels != nil {
		veleroExport.Labels[config.SnapshotExportBackupName] = veleroPlan.Name
	} else {
		veleroExport.Labels = map[string]string{
			config.SnapshotExportBackupName: veleroPlan.Name,
		}
	}

	err := r.Client.Update(context.TODO(), veleroExport)
	if err != nil {
		return err
	}

	veleroExport.Status.VeleroBackupRef = &corev1.ObjectReference{}
	veleroExport.Status.VeleroBackupRef.Name = veleroPlan.Name
	veleroExport.Status.VeleroBackupRef.Namespace = veleroPlan.Namespace
	return nil
}

func (r *VeleroExportReconciler) updateStatus(ctx context.Context, client k8sclient.Client, veleroExport *dmapi.VeleroExport, err error) error {
	logger := log.FromContext(ctx)
	if err != nil {
		veleroExport.Status.Message = err.Error()
		if veleroExport.Status.State != dmapi.StateVeleroFailed {
			veleroExport.Status.State = dmapi.StateFailed
		}
		if veleroExport.Status.LastFailureTimestamp == nil {
			veleroExport.Status.LastFailureTimestamp = &metav1.Time{Time: time.Now()}
		}
		logger.Error(err, "snapshot export failure", "phase", veleroExport.Status.Phase)
	} else {
		if veleroExport.Status.LastFailureTimestamp != nil {
			veleroExport.Status.LastFailureTimestamp = nil
		}
		veleroExport.Status.Message = ""
		if veleroExport.Status.Phase == dmapi.PhaseCreated {
			veleroExport.Status.StartTimestamp = &metav1.Time{Time: time.Now()}
			retention := veleroExport.Spec.Policy.Retention * time.Hour
			veleroExport.Status.StopTimestamp = &metav1.Time{Time: veleroExport.Status.StartTimestamp.Time.Add(retention)}
		}
		veleroExport.Status.Phase = r.nextPhase(veleroExport.Status.Phase)
		if veleroExport.Status.Phase == dmapi.GetLastPhase(veleroExportSteps) {
			veleroExport.Status.State = dmapi.StateCompleted
			veleroExport.Status.CompletionTimestamp = &metav1.Time{Time: time.Now()}
		} else {
			veleroExport.Status.State = dmapi.StateInProgress
		}
	}
	result := r.Client.Status().Update(context.TODO(), veleroExport)
	if result == nil {
		logger.Info("snapshot export status update", "phase", veleroExport.Status.Phase, "state", veleroExport.Status.State)
	}
	return result
}

func (r *VeleroExportReconciler) precheck(client k8sclient.Client, veleroExport *dmapi.VeleroExport, opt *ops.Operation) error {
	vBackupRef := veleroExport.Spec.VeleroBackupRef
	//check veleroBackup Ref
	if !opt.RefSet(vBackupRef) {
		return fmt.Errorf("invalid velero backup ref")
	}
	// get veleroBackup plan
	backup, err := opt.GetBackupPlan(vBackupRef.Name, vBackupRef.Namespace)
	if err != nil || backup.Status.Phase != velero.BackupPhaseCompleted || !*backup.Spec.SnapshotVolumes {
		err = fmt.Errorf("invalid backup plan %s", vBackupRef.Name)
	} else {
		// check if any volumesnapshot
		vsMap := make(map[string]string)
		for _, namespace := range veleroExport.Spec.IncludedNamespaces {
			vsList, err := opt.GetVolumeSnapshotList(vBackupRef.Name, namespace)
			if err != nil {
				err = fmt.Errorf("validate backup failed, could not get volume snapshot for backup plan %s", vBackupRef.Name)
				return err
			} else {
				for _, volumesnapshot := range vsList.Items {
					key := volumesnapshot.Namespace + "/" + *volumesnapshot.Spec.Source.PersistentVolumeClaimName
					vsMap[key] = volumesnapshot.Name
				}
			}
		}

		for key, _ := range veleroExport.Spec.DataSourceMapping {
			if _, ok := vsMap[key]; !ok {
				err = fmt.Errorf("volume snapshot for pvc %s doesn't exist", key)
				return err
			}
		}
	}
	return err
}

func (r *VeleroExportReconciler) validatePolicy(export *dmapi.VeleroExport) error {
	policy := &export.Spec.Policy
	if policy == nil {
		policy = &dmapi.ExportPolicy{
			Retention: dmapi.DefaultExportRetention,
		}
	}
	return nil
}

func (r *VeleroExportReconciler) deleteVeleroExport(export *dmapi.VeleroExport) error {

	// Delete velero export
	err := r.Client.Delete(context.TODO(), export)
	if err != nil {
		return err
	}

	return nil
}

func (r *VeleroExportReconciler) UpdateVsrlAnnotations(vsrl []*ops.VolumeSnapshotResource, namespace string, veleroExport *dmapi.VeleroExport) {

	if veleroExport.Annotations == nil {
		veleroExport.Annotations = make(map[string]string)
	}
	vsrString := ""
	for _, vsr := range vsrl {
		vsrString = vsrString + vsr.VolumeSnapshotName + "," +
			string(vsr.OrigVolumeSnapshotUID) + "," +
			vsr.PersistentVolumeClaimName + "," +
			vsr.VolumeSnapshotContentName + "," +
			string(vsr.NewVolumeSnapshotUID) + "," +
			vsr.PersistentVolumeName
		vsrString += ";"
	}

	veleroExport.Annotations[VolumeSnapshotResourceAnnPrefix+namespace] = vsrString[:(len(vsrString) - 1)]
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
