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

func (o *Operation) GetVeleroBackup(backupName string) (string, error) {
	backups := &velero.BackupList{}
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     config.VeleroNamespace,
		LabelSelector: k8slabels.SelectorFromSet(labels),
	}
	err := o.client.List(context.TODO(), backups, options)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		return "", err
	}
	bp := backups.Items[0]
	return bp.Name, nil
}

func (o *Operation) SyncBackupNamespaceFc(backupName string) (string, error) {
	newBp, err := o.AsyncBackupNamespaceFc(backupName)
	if err != nil {
		return "", err
	}
	// get velero backup plan
	o.GetCompletedBackup(newBp.Name)
	return newBp.Name, nil
}

// Call velero to backup namespace using filesystem copy
func (o *Operation) AsyncBackupNamespaceFc(backupName string) (*velero.Backup, error) {
	// get velero backup plan
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: config.VeleroNamespace,
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

func (o *Operation) GetCompletedBackup(backupName string) {
	bp := &velero.Backup{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: config.VeleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get velero backup plan %s", backupName))
		panic(err)
	}
	if bp.Status.Phase != velero.BackupPhaseCompleted {
		time.Sleep(time.Duration(5) * time.Second)
		o.GetCompletedBackup(backupName)
	}
}

// Restore original namespace using velero
// func restoreNamespace(client k8sclient.Client, backupName string, srcNamespace string, tgtNamespace string) string {
// 	nsMapping := make(map[string]string)
// 	nsMapping[srcNamespace] = tgtNamespace
// 	excludedResources := []string{
// 		"nodes",
// 		"events",
// 		"events.events.k8s.io",
// 		"backups.velero.io",
// 		"restores.velero.io",
// 		"resticrepositories.velero.io",
// 	}
// 	if srcNamespace == tgtNamespace {
// 		excludedResources = append(excludedResources, "persistentvolumeclaims")
// 	}
// 	restore := &velero.Restore{
// 		ObjectMeta: metav1.ObjectMeta{
// 			GenerateName: GenerateRestoreName,
// 			Namespace:    VeleroNamespace,
// 		},
// 		Spec: velero.RestoreSpec{
// 			BackupName:        backupName,
// 			RestorePVs:        &(t),
// 			ExcludedResources: excludedResources,
// 			NamespaceMapping:  nsMapping,
// 		},
// 	}
// 	err := client.Create(context.TODO(), restore)
// 	if err != nil {
// 		fmt.Printf("Failed to create velero restore plan %s \n", restore.Name)
// 		panic(err)
// 	}
// 	fmt.Printf("Created velero restore plan %s \n", restore.Name)

// 	var restoreName string = restore.Name
// 	status := string(restore.Status.Phase)
// 	for status != "Completed" {
// 		restore = &velero.Restore{}
// 		key := k8sclient.ObjectKey{
// 			Name:      restoreName,
// 			Namespace: VeleroNamespace,
// 		}
// 		client.Get(context.Background(), key, restore)
// 		time.Sleep(time.Duration(5) * time.Second)
// 		status = string(restore.Status.Phase)
// 	}
// 	return restore.Name
// }
