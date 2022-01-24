package operation

import (
	"context"
	"fmt"
	"time"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) UpdatePV(PvName string, namespace string) error {
	pv := &core.PersistentVolume{}
	_ = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      PvName,
	}, pv)
	pv.Spec.ClaimRef = nil
	err := o.client.Update(context.TODO(), pv)
	if err != nil {
		if errors.IsConflict(err) {
			o.UpdatePV(PvName, namespace)
		} else {
			o.logger.Info(fmt.Sprintf("Failed to update pv %s to remove reference in namespace %s", PvName, namespace))
			return err
		}
	}
	o.logger.Info(fmt.Sprintf("Update pv %s to remove reference in namespace %s", PvName, namespace))
	return nil
}

func (o *Operation) CreatePvcsWithVs(vsrl []*VolumeSnapshotResource, backupNs string, tgtNs string) error {
	for _, vsr := range vsrl {
		err := o.CreatePvcWithVs(vsr, backupNs, tgtNs)
		if err != nil {
			return err
		}
	}
	return nil
}

// 1. update pv to Retain
// 2. delete original pvc
// 3. update pv to be availble
// 4. create new pvc to reference pv
func (o *Operation) CreatePvcWithVs(vsr *VolumeSnapshotResource, backupNs string, tgtNs string) error {

	// check if pvc already exists
	newPvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: tgtNs,
		Name:      vsr.PersistentVolumeClaimName,
	}, newPvc)
	if err == nil {
		return nil
	}

	// need to get pvc info in backup namespace
	pvc := &core.PersistentVolumeClaim{}
	err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: backupNs,
		Name:      vsr.PersistentVolumeClaimName,
	}, pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", backupNs))
		return err
	}

	var apiGroup = "snapshot.storage.k8s.io"
	newPvc = &core.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: tgtNs,
			Name:      vsr.PersistentVolumeClaimName,
		},
		Spec: core.PersistentVolumeClaimSpec{
			StorageClassName: pvc.Spec.StorageClassName,
			DataSource: &core.TypedLocalObjectReference{
				Name:     vsr.VolumeSnapshotName,
				Kind:     "VolumeSnapshot",
				APIGroup: &apiGroup,
			},
			AccessModes: []core.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: core.ResourceRequirements{
				Requests: pvc.Spec.Resources.Requests,
			},
		},
	}
	err = o.client.Create(context.TODO(), newPvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create pvc in namespace %s", tgtNs))
		return err
	}
	o.logger.Info(fmt.Sprintf("Created pvc %s in namespace %s", vsr.PersistentVolumeClaimName, tgtNs))
	return nil
}

// Create pod with pvc
func (o *Operation) CreatePvcsWithPv(vsrl []*VolumeSnapshotResource, backupNs string, tgtNs string) error {
	for _, vsr := range vsrl {
		err := o.CreatePvcWithPv(vsr, tgtNs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) getPvc(name string, namespace string) (*core.PersistentVolumeClaim, error) {
	pvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", namespace))
		return nil, err
	}
	return pvc, nil
}

func (o *Operation) isPvcDeleted(name string, namespace string) (bool, error) {
	pvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, pvc)
	if err != nil && errors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	deleted, err := o.isPvcDeleted(name, namespace)
	if err == nil && !deleted {
		time.Sleep(time.Duration(2) * time.Second)
		deleted, err = o.isPvcDeleted(name, namespace)
	}
	return true, nil
}

// Create pod with pvc
func (o *Operation) DeletePvcWithVs(vsr *VolumeSnapshotResource, namespace string) error {
	pvc, err := o.getPvc(vsr.PersistentVolumeClaimName, namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", namespace))
		return err
	}

	// pvc already created using pv
	if pvc.Spec.DataSource == nil && pvc.Spec.VolumeName != "" {
		return nil
	}

	if pvc.Spec.VolumeName == "" {
		time.Sleep(time.Duration(5) * time.Second)
		pvc, err = o.getPvc(vsr.PersistentVolumeClaimName, namespace)
		if err != nil {
			o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", namespace))
			return err
		}
	}
	pvName := pvc.Spec.VolumeName
	o.logger.Info(fmt.Sprintf("Get pvc %s and pv %s", vsr.PersistentVolumeClaimName, pvName))

	// patch the pv to Retain
	patch := []byte(`{"spec":{"persistentVolumeReclaimPolicy": "Retain"}}`)
	err = o.client.Patch(context.TODO(), &core.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      pvName,
		},
	}, k8sclient.RawPatch(types.MergePatchType, patch))
	if err != nil {
		o.logger.Error(err, "Failed to patch pv to retain")
		return err
	}
	o.logger.Info(fmt.Sprintf("Patch pv %s with retain option", pvName))

	// delete pvc
	err = o.client.Delete(context.TODO(), pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to delete pvc %s", pvc.Name))
		return err
	}
	_, err = o.isPvcDeleted(vsr.PersistentVolumeClaimName, namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("delete pvc %s failure", pvc.Name))
	}
	o.logger.Info(fmt.Sprintf("Deleted pvc %s", pvc.Name))

	// update pv to remove reference
	err = o.UpdatePV(pvName, namespace)
	if err != nil {
		return err
	}

	// create pvc with volume
	newPvc := &core.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      vsr.PersistentVolumeClaimName,
		},
		Spec: core.PersistentVolumeClaimSpec{
			StorageClassName: pvc.Spec.StorageClassName,
			AccessModes: []core.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: core.ResourceRequirements{
				Requests: pvc.Spec.Resources.Requests,
			},
			VolumeName: pvc.Spec.VolumeName,
		},
	}
	err = o.createPvc(newPvc)
	o.logger.Info(fmt.Sprintf("Create pvc %s in %s with pv %s", vsr.PersistentVolumeClaimName, namespace, pvName))

	err = o.isPVCReady(namespace, vsr.PersistentVolumeClaimName)
	if err != nil {
		o.logger.Error(err, "Get pvc failure")
		return err
	}

	// patch the pv to Delete
	patch = []byte(`{"spec": {"persistentVolumeReclaimPolicy": "Delete"}}`)
	err = o.client.Patch(context.TODO(), &core.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      pvName,
		},
	}, k8sclient.RawPatch(types.MergePatchType, patch))
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to patch pv %s with delete option", pvName))
		return err
	}
	o.logger.Info(fmt.Sprintf("Patch pv %s with delete option \n", pvName))
	return nil
}

func (o *Operation) createPvc(pvc *core.PersistentVolumeClaim) error {
	err := o.client.Create(context.TODO(), pvc)
	if err != nil {
		if errors.IsConflict(err) {
			o.createPvc(pvc)
		} else {
			o.logger.Error(err, fmt.Sprintf("Failed to create pvc in namespace %s", pvc.Namespace))
			return err
		}
	}
	return nil
}

func (o *Operation) isPVCReady(namespace string, PersistentVolumeClaimName string) error {
	pvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      PersistentVolumeClaimName,
	}, pvc)
	if pvc.Status.Phase != "Bound" {
		time.Sleep(time.Duration(1) * time.Second)
		err = o.isPVCReady(namespace, PersistentVolumeClaimName)
		if err != nil {
			return err
		}
	}
	return nil
}
