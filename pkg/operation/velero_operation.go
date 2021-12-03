package operation

import (
	"context"
	"fmt"
	"time"

	config "github.com/jibudata/data-mover/pkg/config"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) GetVeleroBackup(backupName string, veleroNs string) (*velero.Backup, error) {
	backups := &velero.BackupList{}
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     veleroNs,
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

func (o *Operation) GetBackupPlan(backupName string, veleroNs string) (*velero.Backup, error) {
	backup := &velero.Backup{}
	keys := k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNs,
		Name:      backupName,
	}
	err := o.client.Get(context.TODO(), keys, backup)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return nil, err
	}
	return backup, nil
}

func (o *Operation) SyncBackupNamespaceFc(backupName string, veleroNs string) (string, error) {
	newBp, err := o.AsyncBackupNamespaceFc(backupName, veleroNs)
	if err != nil {
		return "", err
	}
	// get velero backup plan
	o.GetCompletedBackup(newBp.Name, veleroNs)
	return newBp.Name, nil
}

// Call velero to backup namespace using filesystem copy
func (o *Operation) AsyncBackupNamespaceFc(backupName string, veleroNs string) (*velero.Backup, error) {
	// get velero backup plan
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: veleroNs,
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
			IncludedNamespaces: []string{o.dmNamespace},
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

func (o *Operation) GetBackupStatus(backupName string, veleroNs string) (velero.BackupPhase, error) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNs,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return "", err
	}
	return bp.Status.Phase, nil
}

func (o *Operation) GetCompletedBackup(backupName string, veleroNs string) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		// Namespace: config.VeleroNamespace,
		Namespace: veleroNs,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		panic(err)
	}
	if bp.Status.Phase != velero.BackupPhaseCompleted {
		time.Sleep(time.Duration(5) * time.Second)
		o.GetCompletedBackup(backupName, veleroNs)
	}
}

// Restore original namespace using velero
func (o *Operation) AsyncRestoreNamespace(backupName string, veleroNs string, srcNamespace string, tgtNamespace string) (*velero.Restore, error) {
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
			Namespace:    veleroNs,
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

func (o *Operation) SyncRestoreNamespace(backupName string, veleroNs string, srcNamespace string, tgtNamespace string) (string, error) {
	restore, err := o.AsyncRestoreNamespace(backupName, veleroNs, srcNamespace, tgtNamespace)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore, veleroNs)
	return restore.Name, nil
}

func (o *Operation) MonitorRestoreNamespace(restore *velero.Restore, veleroNs string) {
	status := string(restore.Status.Phase)
	for status != "Completed" {
		restore = &velero.Restore{}
		key := k8sclient.ObjectKey{
			Name:      restore.Name,
			Namespace: veleroNs,
		}
		o.client.Get(context.Background(), key, restore)
		time.Sleep(time.Duration(5) * time.Second)
		status = string(restore.Status.Phase)
	}
}
