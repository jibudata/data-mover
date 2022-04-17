package operation

import (
	"context"
	"fmt"
	"strconv"
	"time"

	dmapi "github.com/jibudata/data-mover/api/v1alpha1"
	config "github.com/jibudata/data-mover/pkg/config"
	"github.com/jibudata/data-mover/pkg/storageclass"
	"github.com/jibudata/data-mover/pkg/util"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DataImportLabel = "data-import-name"

	ResticRateLimitAnnotation = "velero.io/rate-limit-kb"

	StorageClassPvcMappings = "jibudata.com/pvc-storageclass-plugin"
	StorageClassPvMappings  = "jibudata.com/pv-storageclass-plugin"
)

func (o *Operation) GetVeleroBackup(backupName string, namespace string) (*velero.Backup, error) {
	backups := &velero.BackupList{}
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     namespace,
		LabelSelector: k8slabels.SelectorFromSet(labels),
	}
	err := o.client.List(context.TODO(), backups, options)
	if err != nil && !errors.IsNotFound(err) {
		msg := fmt.Sprintf("failed to list velero backup plan %s", backupName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	if len(backups.Items) > 0 {
		return &backups.Items[0], nil
	}

	return nil, nil
}

func (o *Operation) GetBackupPlan(backupName string, namespace string) (*velero.Backup, error) {
	backup := &velero.Backup{}
	keys := k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: namespace,
		Name:      backupName,
	}
	err := o.client.Get(context.TODO(), keys, backup)
	if err != nil {
		msg := fmt.Sprintf("failed to get velero backup plan %s", backupName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	return backup, nil
}

func (o *Operation) SyncBackupNamespaceFc(backupName string, namespace string, includedNamespaces []string) (string, error) {
	newBp, err := o.EnsureVeleroBackup(backupName, namespace, "", includedNamespaces)
	if err != nil {
		return "", err
	}
	// get velero backup plan
	o.GetCompletedBackup(newBp.Name, namespace)
	return newBp.Name, nil
}

// Call velero to backup namespace using filesystem copy
func (o *Operation) EnsureVeleroBackup(backupName, namespace, rateLimit string, includedNamespaces []string) (*velero.Backup, error) {
	// get velero backup plan
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		msg := fmt.Sprintf("failed to get velero backup plan %s", backupName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	o.logger.Info(fmt.Sprintf("Get velero backup plan %s", backupName))
	labels := map[string]string{
		config.VeleroStorageLabel: bp.Labels[config.VeleroStorageLabel],
		config.VeleroBackupLabel:  backupName,
	}
	annotations := map[string]string{
		config.VeleroSrcClusterGitAnn: bp.Annotations[config.VeleroSrcClusterGitAnn],
		config.VeleroK8sMajorVerAnn:   bp.Annotations[config.VeleroK8sMajorVerAnn],
		config.VeleroK8sMinorVerAnn:   bp.Annotations[config.VeleroK8sMinorVerAnn],
	}
	if len(rateLimit) > 0 {
		annotations[ResticRateLimitAnnotation] = rateLimit
	}
	newBpName := config.VeleroBackupNamePrefix + backupName
	var newBp *velero.Backup = &velero.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Name:        newBpName,
			Namespace:   bp.Namespace,
			Annotations: annotations,
		},
		Spec: velero.BackupSpec{
			// IncludeClusterResources: includeClusterResources,
			StorageLocation:    bp.Spec.StorageLocation,
			TTL:                bp.Spec.TTL,
			IncludedNamespaces: includedNamespaces,
			Hooks: velero.BackupHooks{
				Resources: []velero.BackupResourceHookSpec{},
			},
			SnapshotVolumes:        &(config.False),
			DefaultVolumesToRestic: &(config.True),
		},
	}
	err = o.client.Create(context.TODO(), newBp)
	if err != nil && errors.IsAlreadyExists(err) {
		o.logger.Info("velero plan already exists", "plan name", newBpName)
		return newBp, nil
	} else if err != nil {
		msg := fmt.Sprintf("failed to create velero backup plan %s", newBpName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	o.logger.Info(fmt.Sprintf("Created velero backup plan %s", newBpName))
	return newBp, nil
}

func (o *Operation) GetBackupStatus(backupName string, namespace string) (velero.BackupPhase, error) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: namespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		msg := fmt.Sprintf("failed to get velero backup plan %s", backupName)
		o.logger.Error(err, msg)
		return "", util.WrapError(msg, err)
	}
	return bp.Status.Phase, nil
}

func (o *Operation) GetCompletedBackup(backupName string, namespace string) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: namespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		panic(err)
	}
	if bp.Status.Phase != velero.BackupPhaseCompleted {
		time.Sleep(time.Duration(5) * time.Second)
		o.GetCompletedBackup(backupName, namespace)
	}
	// TBD: add timeout
}

func (o *Operation) EnsureVeleroRestore(veleroImport *dmapi.VeleroImport, backupName, namespace, dataImport, rateLimit string, nsMapping map[string]string, excludePV bool) (*velero.Restore, error) {

	excludedResources := []string{
		"nodes",
		"events",
		"events.events.k8s.io",
		"backups.velero.io",
		"restores.velero.io",
		"resticrepositories.velero.io",
	}
	if excludePV {
		excludedResources = append(excludedResources, "persistentvolumeclaims")
		excludedResources = append(excludedResources, "persistentvolumes")
	}

	var includedNamespaces []string
	for key := range nsMapping {
		includedNamespaces = append(includedNamespaces, key)
	}

	var annotations map[string]string
	if len(rateLimit) > 0 {
		annotations = make(map[string]string)
		annotations[ResticRateLimitAnnotation] = rateLimit
	}

	// check storageclass mapping
	storageClassActions := veleroImport.Spec.Actions.StorageClassMappings
	if storageClassActions != nil {
		if annotations == nil {
			annotations = make(map[string]string)
		}

		value, err := storageclass.CreateAnnotationFromMap(storageClassActions)
		if err != nil {
			return nil, err
		}
		annotations[StorageClassPvMappings] = value
		annotations[StorageClassPvcMappings] = value
	}

	restoreName := config.VeleroRestoreNamePrefix + dataImport
	restore := &velero.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:        restoreName,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: velero.RestoreSpec{
			BackupName:         backupName,
			RestorePVs:         &(config.True),
			ExcludedResources:  excludedResources,
			NamespaceMapping:   nsMapping,
			IncludedNamespaces: includedNamespaces,
		},
	}
	err := o.client.Create(context.TODO(), restore)

	if err != nil && errors.IsAlreadyExists(err) {
		return restore, nil
	} else if err != nil {
		msg := fmt.Sprintf("failed to create velero restore plan %s", restoreName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	o.logger.Info(fmt.Sprintf("Created velero restore plan %s", restoreName))
	return restore, nil
}

func (o *Operation) SyncRestoreNamespaces(veleroImport *dmapi.VeleroImport, backupName string, namespace string, nsMapping map[string]string, excludePV bool, dataImport string) (string, error) {
	restore, err := o.EnsureVeleroRestore(veleroImport, backupName, namespace, dataImport, "", nsMapping, excludePV)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore.Name, namespace)
	return restore.Name, nil
}

// Restore original namespace using velero, only for CLIpkg/config/constants.go
func (o *Operation) CreateVeleroRestore(backupName string, namespace string, srcNamespace string, tgtNamespace string) (*velero.Restore, error) {
	nsMapping := make(map[string]string)
	nsMapping[srcNamespace] = tgtNamespace
	excludedResources := []string{
		"nodes",
		"events",
		"events.events.k8s.io",
		"backups.velero.io",
		"restores.velero.io",
		"resticrepositories.velero.io",
	}
	if srcNamespace == tgtNamespace {
		excludedResources = append(excludedResources, "persistentvolumeclaims")
		excludedResources = append(excludedResources, "persistentvolumes")
	}
	suffix := o.GetRestoreJobSuffix(nil)
	restoreName := config.VeleroRestoreNamePrefix + suffix
	restore := &velero.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: namespace,
		},
		Spec: velero.RestoreSpec{
			BackupName:        backupName,
			RestorePVs:        &(config.True),
			ExcludedResources: excludedResources,
			NamespaceMapping:  nsMapping,
		},
	}
	err := o.client.Create(context.TODO(), restore)
	if err != nil && errors.IsAlreadyExists(err) {
		return restore, nil
	} else if err != nil {
		msg := fmt.Sprintf("failed to create velero restore plan %s", restoreName)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	o.logger.Info(fmt.Sprintf("Created velero restore plan %s", restoreName))
	return restore, nil
}

func (o *Operation) SyncRestoreNamespace(backupName string, namespace string, srcNamespace string, tgtNamespace string) (string, error) {
	restore, err := o.CreateVeleroRestore(backupName, namespace, srcNamespace, tgtNamespace)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore.Name, namespace)
	return restore.Name, nil
}

func (o *Operation) GetVeleroRestore(restoreName string, namespace string) *velero.Restore {
	restore := &velero.Restore{}
	key := k8sclient.ObjectKey{
		Name:      restoreName,
		Namespace: namespace,
	}
	o.client.Get(context.TODO(), key, restore)
	return restore
}

func (o *Operation) IsNamespaceRestored(restoreName string, namespace string) bool {
	restore := o.GetVeleroRestore(restoreName, namespace)
	status := string(restore.Status.Phase)
	return status == string(velero.BackupPhaseCompleted) || status == string(velero.BackupPhasePartiallyFailed)
}

func (o *Operation) MonitorRestoreNamespace(restoreName string, namespace string) {
	restored := false
	for !restored {
		restored = o.IsNamespaceRestored(restoreName, namespace)
		time.Sleep(time.Duration(5) * time.Second)
	}
}

// Get velero Backup
func (o *Operation) GetVeleroBackupUID(client k8sclient.Client, jobUID types.UID) (*velero.Backup, error) {
	o.logger.Info("GetVeleroBackup", "uid", string(jobUID))
	labels := make(map[string]string)
	labels["migration-initial-backup"] = string(jobUID)

	list := velero.BackupList{}
	err := client.List(
		context.TODO(),
		&list,
		k8sclient.MatchingLabels(labels))
	if err != nil {
		msg := fmt.Sprintf("failed to list velero backup plan with uid %s", jobUID)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	o.logger.Info("GetVeleroBackup", "list", list)
	if len(list.Items) > 0 {
		o.logger.Info("GetVeleroBackup", "&list.Items[0", &list.Items[0])
		return &list.Items[0], nil
	}

	return nil, nil
}

func (o *Operation) GetRestoreJobSuffix(veleroImport *dmapi.VeleroImport) string {
	var suffix string
	if veleroImport == nil || !o.RefSet(veleroImport.Spec.DataImportRef) {
		suffix = strconv.FormatUint(uint64(time.Now().Unix()), 10)
	} else {
		suffix = veleroImport.Spec.DataImportRef.Name
	}
	return suffix
}
