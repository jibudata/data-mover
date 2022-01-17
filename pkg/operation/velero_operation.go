package operation

import (
	"context"
	"fmt"
	"time"

	config "github.com/jibudata/data-mover/pkg/config"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) GetVeleroBackup(backupName string, veleroNamespace string) (*velero.Backup, error) {
	backups := &velero.BackupList{}
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     veleroNamespace,
		LabelSelector: k8slabels.SelectorFromSet(labels),
	}
	err := o.client.List(context.TODO(), backups, options)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return nil, err
	}
	bp := backups.Items[0]
	return &bp, nil
}

func (o *Operation) GetBackupPlan(backupName string, veleroNamespace string) (*velero.Backup, error) {
	backup := &velero.Backup{}
	keys := k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNamespace,
		Name:      backupName,
	}
	err := o.client.Get(context.TODO(), keys, backup)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return nil, err
	}
	return backup, nil
}

func (o *Operation) SyncBackupNamespaceFc(backupName string, veleroNamespace string, includedNamespaces []string) (string, error) {
	newBp, err := o.AsyncBackupNamespaceFc(backupName, veleroNamespace, includedNamespaces)
	if err != nil {
		return "", err
	}
	// get velero backup plan
	o.GetCompletedBackup(newBp.Name, veleroNamespace)
	return newBp.Name, nil
}

// Call velero to backup namespace using filesystem copy
func (o *Operation) AsyncBackupNamespaceFc(backupName string, veleroNamespace string, includedNamespaces []string) (*velero.Backup, error) {
	// get velero backup plan
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: veleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return nil, err
	}
	o.logger.Info(fmt.Sprintf("Get velero backup plan %s", backupName))
	labels := map[string]string{
		config.VeleroStorageLabel: bp.Labels[config.VeleroStorageLabel],
		config.VeleroBackupLabel:  backupName,
	}
	annotation := map[string]string{
		config.VeleroSrcClusterGitAnn: bp.Annotations[config.VeleroSrcClusterGitAnn],
		config.VeleroK8sMajorVerAnn:   bp.Annotations[config.VeleroK8sMajorVerAnn],
		config.VeleroK8sMinorVerAnn:   bp.Annotations[config.VeleroK8sMinorVerAnn],
	}
	var newBp *velero.Backup = &velero.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Labels:       labels,
			GenerateName: config.GenerateBackupName,
			Namespace:    bp.Namespace,
			Annotations:  annotation,
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
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create velero backup plan %s", newBp.Name))
		return nil, err
	}
	o.logger.Info(fmt.Sprintf("Created velero backup plan %s", newBp.Name))
	return newBp, nil
}

func (o *Operation) GetBackupStatus(backupName string, veleroNamespace string) (velero.BackupPhase, error) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return "", err
	}
	return bp.Status.Phase, nil
}

func (o *Operation) GetCompletedBackup(backupName string, veleroNamespace string) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		panic(err)
	}
	if bp.Status.Phase != velero.BackupPhaseCompleted {
		time.Sleep(time.Duration(5) * time.Second)
		o.GetCompletedBackup(backupName, veleroNamespace)
	}
	// TBD: add timeout
}

func (o *Operation) AsyncRestoreNamespaces(backupName string, veleroNamespace string, namespaceMapping map[string]string, excludePV bool) (*velero.Restore, error) {

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
	}
	restore := &velero.Restore{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: config.GenerateRestoreName,
			Namespace:    veleroNamespace,
		},
		Spec: velero.RestoreSpec{
			BackupName:        backupName,
			RestorePVs:        &(config.True),
			ExcludedResources: excludedResources,
			NamespaceMapping:  namespaceMapping,
		},
	}
	err := o.client.Create(context.TODO(), restore)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create velero restore plan %s", restore.Name))
		return nil, err
	}
	o.logger.Info(fmt.Sprintf("Created velero restore plan %s", restore.Name))
	return restore, nil
}

func (o *Operation) SyncRestoreNamespaces(backupName string, veleroNamespace string, namespaceMapping map[string]string, excludePV bool) (string, error) {
	restore, err := o.AsyncRestoreNamespaces(backupName, veleroNamespace, namespaceMapping, excludePV)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore.Name, veleroNamespace)
	return restore.Name, nil
}

// Restore original namespace using velero
func (o *Operation) AsyncRestoreNamespace(backupName string, veleroNamespace string, srcNamespace string, tgtNamespace string) (*velero.Restore, error) {
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
	}
	restore := &velero.Restore{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: config.GenerateRestoreName,
			Namespace:    veleroNamespace,
		},
		Spec: velero.RestoreSpec{
			BackupName:        backupName,
			RestorePVs:        &(config.True),
			ExcludedResources: excludedResources,
			NamespaceMapping:  nsMapping,
		},
	}
	err := o.client.Create(context.TODO(), restore)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create velero restore plan %s", restore.Name))
		return nil, err
	}
	o.logger.Info(fmt.Sprintf("Created velero restore plan %s", restore.Name))
	return restore, nil
}

func (o *Operation) SyncRestoreNamespace(backupName string, veleroNamespace string, srcNamespace string, tgtNamespace string) (string, error) {
	restore, err := o.AsyncRestoreNamespace(backupName, veleroNamespace, srcNamespace, tgtNamespace)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore.Name, veleroNamespace)
	return restore.Name, nil
}

func (o *Operation) GetVeleroRestore(restoreName string, veleroNamespace string) *velero.Restore {
	restore := &velero.Restore{}
	key := k8sclient.ObjectKey{
		Name:      restoreName,
		Namespace: veleroNamespace,
	}
	o.client.Get(context.TODO(), key, restore)
	return restore
}

func (o *Operation) IsNamespaceRestored(restoreName string, veleroNamespace string) bool {
	restore := o.GetVeleroRestore(restoreName, veleroNamespace)
	status := string(restore.Status.Phase)
	return status == string(velero.BackupPhaseCompleted) || status == string(velero.BackupPhasePartiallyFailed)
}

func (o *Operation) MonitorRestoreNamespace(restoreName string, veleroNamespace string) {
	restored := false
	for !restored {
		restored = o.IsNamespaceRestored(restoreName, veleroNamespace)
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
		o.logger.Info("error", "err", err)
		return nil, err
	}
	o.logger.Info("GetVeleroBackup", "list", list)
	if len(list.Items) > 0 {
		o.logger.Info("GetVeleroBackup", "&list.Items[0", &list.Items[0])
		return &list.Items[0], nil
	}

	return nil, nil
}
