package operation

import (
	"context"
	"fmt"
	"time"

	config "github.com/jibudata/data-mover/pkg/config"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) CreateNamespace(ns string, forceRecreate bool) error {
	namespace := core.Namespace{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: ns,
		Name:      ns,
	}, &namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			//create namespace and return
			namespace = core.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}
			err = o.client.Create(context.TODO(), &namespace)
			if err != nil {
				o.logger.Error(err, fmt.Sprintf("Failed to delete namespace %s", ns))
				return err
			}
			return nil

		} else {
			o.logger.Error(err, fmt.Sprintf("Failed to get namespace %s", ns))
			return err
		}
	}
	if !forceRecreate {
		// If namespace exist and do not force recreate namespace, skip recreate
		return nil
	}
	err = o.client.Delete(context.TODO(), &namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to delete namespace %s", ns))
		return err
	}
	err = nil
	for err == nil {
		namespace := core.Namespace{}
		err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: ns,
			Name:      ns,
		}, &namespace)
		time.Sleep(time.Duration(15) * time.Second)
	}
	namespace = core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}
	err = o.client.Create(context.TODO(), &namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create namespace %s", ns))
		return err
	}
	return nil
}

func (o *Operation) AsyncDeleteNamespace(ns string) error {
	namespace := &core.Namespace{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Name: ns,
	}, namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get namespace %s", ns))
		// If namespace not exist, skip
		if serr, ok := err.(*errors.StatusError); ok {
			if serr.Status().Reason == "NotFound" {
				o.logger.Error(err, fmt.Sprintf("Skip deleting non-existing namespace %s", ns))
				return nil
			}
		}
		return err
	}
	o.client.Delete(context.TODO(), namespace)
	err = nil
	for err == nil {
		time.Sleep(time.Duration(15) * time.Second)
		namespace := core.Namespace{}
		err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: ns,
			Name:      ns,
		}, &namespace)
	}
	return nil
}

func (o *Operation) SyncDeleteNamespace(namespace string) error {
	err := o.AsyncDeleteNamespace(namespace)
	if err != nil {
		return err
	}
	err = o.MonitorDeleteNamespace(namespace)
	return err
}

func (o *Operation) MonitorDeleteNamespace(namespace string) error {
	var err error
	err = nil
	for err == nil {
		time.Sleep(time.Duration(15) * time.Second)
		ns := &core.Namespace{}
		err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: namespace,
			Name:      namespace,
		}, ns)
	}
	return err
}

// Restore original namespace using velero
func (o *Operation) AsyncRestoreNamespace(backupName string, srcNamespace string, tgtNamespace string) (*velero.Restore, error) {
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
			Namespace:    config.VeleroNamespace,
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

func (o *Operation) SyncRestoreNamespace(backupName string, srcNamespace string, tgtNamespace string) (string, error) {
	restore, err := o.AsyncRestoreNamespace(backupName, srcNamespace, tgtNamespace)
	if err != nil {
		return "", err
	}
	o.MonitorRestoreNamespace(restore)
	return restore.Name, nil
}

func (o *Operation) MonitorRestoreNamespace(restore *velero.Restore) {
	status := string(restore.Status.Phase)
	for status != "Completed" {
		restore = &velero.Restore{}
		key := k8sclient.ObjectKey{
			Name:      restore.Name,
			Namespace: config.VeleroNamespace,
		}
		o.client.Get(context.Background(), key, restore)
		time.Sleep(time.Duration(5) * time.Second)
		status = string(restore.Status.Phase)
	}
}
