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

func (o *Operation) UpdatePV(PvName string, tempNs string) error {
	pv := &core.PersistentVolume{}
	_ = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: tempNs,
		Name:      PvName,
	}, pv)
	pv.Spec.ClaimRef = nil
	err := o.client.Update(context.TODO(), pv)
	if err != nil {
		if errors.IsConflict(err) {
			o.UpdatePV(PvName, tempNs)
		} else {
			o.logger.Info(fmt.Sprintf("Failed to update pv %s to remove reference in namespace %s", PvName, tempNs))
			return err
		}
	}
	o.logger.Info(fmt.Sprintf("Update pv %s to remove reference in namespace %s", PvName, tempNs))
	return nil
}

func (o *Operation) CreatePvcsWithVs(vsrl []*VolumeSnapshotResource, backupNs string, tempNs string) error {
	for _, vsr := range vsrl {
		err := o.CreatePvcWithVs(vsr, backupNs, tempNs)
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
func (o *Operation) CreatePvcWithVs(vsr *VolumeSnapshotResource, backupNs string, tempNs string) error {

	// check if pvc already exists
	newPvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: tempNs,
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
			Namespace: tempNs,
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
	err = o.client.Create(context.Background(), newPvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create pvc in namespace %s", tempNs))
		return err
	}
	o.logger.Info(fmt.Sprintf("Created pvc %s in namespace %s", vsr.PersistentVolumeClaimName, tempNs))
	return nil
}

// Create pod with pvc
func (o *Operation) CreatePvcsWithPv(vsrl []*VolumeSnapshotResource, backupNs string, tempNs string) error {
	for _, vsr := range vsrl {
		err := o.CreatePvcWithPv(vsr, tempNs)
		if err != nil {
			return err
		}
	}
	return nil
}

// Create pod with pvc
func (o *Operation) CreatePvcWithPv(vsr *VolumeSnapshotResource, tempNs string) error {

	pvc := &core.PersistentVolumeClaim{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: tempNs,
		Name:      vsr.PersistentVolumeClaimName,
	}, pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", tempNs))
		return err
	}

	// pvc already created using pv
	if pvc.Spec.DataSource == nil && pvc.Spec.VolumeName != "" {
		return nil
	}

	if pvc.Spec.VolumeName == "" {
		time.Sleep(time.Duration(5) * time.Second)
		err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: tempNs,
			Name:      vsr.PersistentVolumeClaimName,
		}, pvc)
		if err != nil {
			o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", tempNs))
			return err
		}
	}
	pvName := pvc.Spec.VolumeName
	o.logger.Info(fmt.Sprintf("Get pvc %s and pv %s", vsr.PersistentVolumeClaimName, pvName))

	// patch the pv to Retain
	patch := []byte(`{"spec":{"persistentVolumeReclaimPolicy": "Retain"}}`)
	err = o.client.Patch(context.Background(), &core.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: tempNs,
			Name:      pvName,
		},
	}, k8sclient.RawPatch(types.MergePatchType, patch))
	if err != nil {
		o.logger.Error(err, "Failed to patch pv to retain")
		return err
	}
	o.logger.Info(fmt.Sprintf("Patch pv %s with retain option", pvName))

	// delete pvc
	err = o.client.Delete(context.Background(), pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to delete pvc %s", pvc.Name))
		return err
	}
	o.logger.Info(fmt.Sprintf("Deleted pvc %s", pvc.Name))
	// update pv to remove reference
	err = o.UpdatePV(pvName, tempNs)
	if err != nil {
		return err
	}

	// create pvc with volume
	newPvc := &core.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: tempNs,
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
	err = o.client.Create(context.Background(), newPvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create pvc in namespace %s", tempNs))
		return err
	}
	o.logger.Info(fmt.Sprintf("Create pvc %s in %s with pv %s", vsr.PersistentVolumeClaimName, tempNs, pvName))

	// patch the pv to Delete
	patch = []byte(`{"spec": {"persistentVolumeReclaimPolicy": "Delete"}}`)
	err = o.client.Patch(context.Background(), &core.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: tempNs,
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
