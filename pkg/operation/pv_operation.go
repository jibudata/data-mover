package operation

import (
	"context"
	"fmt"
	"time"

	core "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) UpdatePvClaimRef(vsrl []*VolumeSnapshotResource, namespace string) error {
	for _, vsr := range vsrl {
		pvc, err := o.getPvc(vsr.PersistentVolumeClaimName, namespace)
		if err != nil {
			o.logger.Error(err, "failed to get pvc")
			return err
		}
		err = o.updatePvClaimRef(vsr.PersistentVolumeName, namespace, pvc)
		if err != nil {
			o.logger.Error(err, "update pvc claimref failed")
			return err
		}
	}
	return nil
}

func (o *Operation) updatePvClaimRef(PvName string, namespace string, pvc *core.PersistentVolumeClaim) error {
	pv := &core.PersistentVolume{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      PvName,
	}, pv)
	if err != nil {
		return err
	}
	pv.Spec.ClaimRef = &core.ObjectReference{
		Name:      pvc.Name,
		Namespace: namespace,
		Kind:      "PersistentVolumeClaim",
		UID:       pvc.UID,
	}
	err = o.client.Update(context.TODO(), pv)
	if err != nil {
		o.logger.Error(err, "Failed to update pv claimRef", "pv", PvName, "pvc reference", pvc.Name, "namespace", namespace)
		return err
	}
	o.logger.Info("successfuly update pv claimRef", "pv", PvName, "pvc reference", pvc.Name, "namespace", namespace)
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
		o.logger.Error(err, fmt.Sprintf("failed to get pvc in namespace %s", backupNs))
		return err
	}

	vs, err := o.GetVolumeSnapshot(vsr.VolumeSnapshotName, tgtNs)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("failed to get vs in namespace %s", backupNs))
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
			AccessModes: pvc.Spec.AccessModes,
			Resources:   pvc.Spec.Resources,
			Selector:    pvc.Spec.Selector,
		},
	}


	storageReq, exists := newPvc.Spec.Resources.Requests[core.ResourceStorage]

	// It is possible that the volume provider allocated a larger capacity volume than what was requested in the backed up PVC.
	// In this scenario the volumesnapshot of the PVC will endup being larger than its requested storage size.
	// Such a PVC, on restore as-is, will be stuck attempting to use a Volumesnapshot as a data source for a PVC that
	// is not large enough.
	// To counter that, here we set the storage request on the PVC to the larger of the PVC's storage request and the size of the
	// VolumeSnapshot
	if vs.Status != nil &&
		vs.Status.RestoreSize != nil &&
		(!exists || vs.Status.RestoreSize.Cmp(storageReq) > 0) {
		newPvc.Spec.Resources.Requests[core.ResourceStorage] = *vs.Status.RestoreSize
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
		err := o.CreatePvcWithPv(vsr, backupNs, tgtNs)
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
	return false, nil
}

func (o *Operation) IsPvcDeleted(vsrl []*VolumeSnapshotResource, namespace string) (bool, error) {
	for _, vsr := range vsrl {
		deleted, err := o.isPvcDeleted(vsr.PersistentVolumeClaimName, namespace)
		if err != nil {
			return false, err
		}
		if !deleted {
			return false, fmt.Errorf("pvc still exists")
		}
	}
	return true, nil
}

func (o *Operation) UpdatePvClaimRetain(vsrl []*VolumeSnapshotResource, namespace string) error {
	return o.updatePvClaimPolicy(vsrl, namespace, core.PersistentVolumeReclaimRetain)
}

func (o *Operation) UpdatePvClaimDelete(vsrl []*VolumeSnapshotResource, namespace string) error {
	return o.updatePvClaimPolicy(vsrl, namespace, core.PersistentVolumeReclaimDelete)
}

func (o *Operation) updatePvClaimPolicy(vsrl []*VolumeSnapshotResource, namespace string, policy core.PersistentVolumeReclaimPolicy) error {
	for _, vsr := range vsrl {
		pvc, err := o.getPvc(vsr.PersistentVolumeClaimName, namespace)
		if err != nil {
			o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", namespace))
			return err
		}
		pvName := pvc.Spec.VolumeName
		o.logger.Info(fmt.Sprintf("Get pvc %s and pv %s", vsr.PersistentVolumeClaimName, pvName))

		// patch the pv to Retain
		err = o.updatePVClaimPolicy(pvName, policy)
		if err != nil {
			o.logger.Error(err, "Failed to patch pv to retain")
			return err
		}
	}
	return nil
}


func (o *Operation) DeletePvc(vsrl []*VolumeSnapshotResource, namespace string) error {

	for _, vsr := range vsrl {
		err := o.deletePvc(vsr.PersistentVolumeClaimName, namespace)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (o *Operation) deletePvc(pvcName string, namespace string) error {
	pvc, err := o.getPvc(pvcName, namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pvc in namespace %s", namespace))
		return err
	}
	err = o.client.Delete(context.TODO(), pvc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to delete pvc %s", pvc.Name))
		return err
	}
	return nil
}

func (o *Operation) EnsurePvcDeleted(namespace string) error {

	pvcList, err := o.getPvcList(namespace)
	if err != nil {
		return err
	}
	if len(pvcList) > 0 {
		return fmt.Errorf("pvc still exists")
	}
	return nil
}

func (o *Operation) getPvcList(namespace string) ([]core.PersistentVolumeClaim, error) {

	pvcList := &core.PersistentVolumeClaimList{}
	options := &k8sclient.ListOptions{
		Namespace: namespace,
	}
	err := o.client.List(context.TODO(), pvcList, options)
	if err != nil {
		return nil, err
	}
	return pvcList.Items, nil

}

// Create pod with pvc
func (o *Operation) CreatePvcWithPv(vsr *VolumeSnapshotResource, namespace, tgtNamespace string) error {
	var err error
	pvc, err := o.getPvc(vsr.PersistentVolumeClaimName, tgtNamespace)
	if err == nil && pvc.Spec.VolumeName == vsr.PersistentVolumeName {
		return nil
	}

	pvc, err = o.getPvc(vsr.PersistentVolumeClaimName, namespace)
	if err != nil {
		return err
	}

	vs, err := o.GetVolumeSnapshot(vsr.VolumeSnapshotName, tgtNamespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("failed to get vs in namespace %s", tgtNamespace))
		return err
	}

	// create pvc with volume
	newPvc := &core.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: tgtNamespace,
			Name:      vsr.PersistentVolumeClaimName,
		},
		Spec: core.PersistentVolumeClaimSpec{
			StorageClassName: pvc.Spec.StorageClassName,
			AccessModes:      pvc.Spec.AccessModes,
			Resources:        pvc.Spec.Resources,
			VolumeName:       vsr.PersistentVolumeName,
			Selector:         pvc.Spec.Selector,
		},
	}
	storageReq, exists := newPvc.Spec.Resources.Requests[core.ResourceStorage]

	// It is possible that the volume provider allocated a larger capacity volume than what was requested in the backed up PVC.
	// In this scenario the volumesnapshot of the PVC will endup being larger than its requested storage size.
	// Such a PVC, on restore as-is, will be stuck attempting to use a Volumesnapshot as a data source for a PVC that
	// is not large enough.
	// To counter that, here we set the storage request on the PVC to the larger of the PVC's storage request and the size of the
	// VolumeSnapshot
	if vs.Status != nil &&
		vs.Status.RestoreSize != nil &&
		(!exists || vs.Status.RestoreSize.Cmp(storageReq) > 0) {
		newPvc.Spec.Resources.Requests[core.ResourceStorage] = *vs.Status.RestoreSize
	}

	err = o.createPvc(newPvc)
	if err != nil {
		o.logger.Error(err, "Failed to create PVC", "name", newPvc.Name)
		return err
	}
	o.logger.Info(fmt.Sprintf("Create pvc %s in %s with pv %s", vsr.PersistentVolumeClaimName, namespace, vsr.PersistentVolumeName))

	return nil
}

func (o *Operation) updatePVClaimPolicy(pvName string, policy core.PersistentVolumeReclaimPolicy) error {

	pv := &core.PersistentVolume{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{Name: pvName}, pv)
	if err != nil {
		o.logger.Error(err, "failed to get pv")
		return err
	}
	pv.Spec.PersistentVolumeReclaimPolicy = policy
	err = o.client.Update(context.TODO(), pv)
	if err != nil {
		o.logger.Error(err, "failed to update pv policy", "new policy", policy)
		return err
	}
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
	if err != nil {
		return err
	}
	if pvc.Status.Phase != "Bound" {
		time.Sleep(time.Duration(1) * time.Second)
		err = o.isPVCReady(namespace, PersistentVolumeClaimName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) CheckPVCReady(namespace string, vsrl []*VolumeSnapshotResource) (bool, error) {
	for _, vsr := range vsrl {
		pvc := &core.PersistentVolumeClaim{}
		err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: namespace,
			Name:      vsr.PersistentVolumeClaimName,
		}, pvc)
		if err != nil {
			return false, err
		}

		if pvc.Status.Phase != core.ClaimBound {
			o.logger.Info("pvc is not Bound", "pvc", pvc.Name)
			return false, nil
		}
		vsr.PersistentVolumeName = pvc.Spec.VolumeName
	}

	return true, nil
}
