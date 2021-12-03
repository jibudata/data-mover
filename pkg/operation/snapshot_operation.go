package operation

import (
	"context"
	"fmt"
	"time"

	config "github.com/jibudata/data-mover/pkg/config"
	snapshotv1beta1api "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type VolumeSnapshotResource struct {
	VolumeSnapshotName string
	VolumeSnapshotUID  types.UID
	// persistentVolumeClaimName specifies the name of the PersistentVolumeClaim
	// object in the same namespace as the VolumeSnapshot object where the
	// snapshot should be dynamically taken from.
	PersistentVolumeClaimName string
	// volumeSnapshotContentName specifies the name of a pre-existing VolumeSnapshotContent
	// object.
	VolumeSnapshotContentName string
}

// 1: get related VolumeSnapshotResource with backup and namespace
// 2. delete vs
// 3. create new vs in poc namespace
func (o *Operation) CreateVolumeSnapshots(backupName string, ns string) ([]*VolumeSnapshotResource, error) {
	volumeSnapshotList, err := o.GetVolumeSnapshotList(backupName, ns)
	if err != nil {
		o.logger.Error(err, "Failed to get volume snapshot list")
		return nil, err
	}
	var vsrl = make([]*VolumeSnapshotResource, len(volumeSnapshotList.Items))
	var newVs = make([]*snapshotv1beta1api.VolumeSnapshot, len(volumeSnapshotList.Items))
	var i int
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	for _, vs := range volumeSnapshotList.Items {
		vsr, newV, err := o.CreateVolumeSnapshot(vs, labels)
		if err != nil {
			o.logger.Error(err, "Failed to create volume snapshot list", "for vs", vs.Name)
			return nil, err
		}
		vsrl[i] = vsr
		newVs[i] = newV
		i++
	}
	return vsrl, nil
}

func (o *Operation) DeleteVolumeSnapshot(vs snapshotv1beta1api.VolumeSnapshot) error {
	// delete volumesnap shot
	err := o.client.Delete(context.TODO(), &vs)
	if err != nil {
		o.logger.Error(err, "Failed to delete volume snapshot")
		return err
	}
	return nil
}

func (o *Operation) CreateVolumeSnapshot(vs snapshotv1beta1api.VolumeSnapshot, labels map[string]string) (*VolumeSnapshotResource, *snapshotv1beta1api.VolumeSnapshot, error) {
	volumeSnapshotName := vs.Name
	uid := vs.UID
	pvc := vs.Spec.Source.PersistentVolumeClaimName
	volumeSnapshotContentName := vs.Status.BoundVolumeSnapshotContentName
	o.logger.Info(fmt.Sprintf("name: %s, uid: %s, pvc: %s, content_name: %s", volumeSnapshotName, uid, *pvc, *volumeSnapshotContentName))
	// create new volumesnap shot
	newV := &snapshotv1beta1api.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: o.dmNamespace,
			Name:      volumeSnapshotName,
			Labels:    labels,
		},
		Spec: snapshotv1beta1api.VolumeSnapshotSpec{
			Source: snapshotv1beta1api.VolumeSnapshotSource{
				VolumeSnapshotContentName: volumeSnapshotContentName,
			},
			VolumeSnapshotClassName: vs.Spec.VolumeSnapshotClassName,
		},
	}
	o.logger.Info(fmt.Sprintf("Created volumesnapshot: %s in %s", volumeSnapshotName, o.dmNamespace))
	err := o.client.Create(context.Background(), newV)
	if err != nil {
		o.logger.Info(fmt.Sprintf("Failed to create volume snapshot in %s", o.dmNamespace))
		return nil, nil, err
	}
	// construct VolumeSnapshotResource
	vsr := &VolumeSnapshotResource{VolumeSnapshotName: volumeSnapshotName,
		VolumeSnapshotUID:         newV.UID,
		PersistentVolumeClaimName: *pvc,
		VolumeSnapshotContentName: *volumeSnapshotContentName}
	return vsr, newV, nil
}

// Update volumesnapshot content to new volumesnapshot in temporary namespace
func (o *Operation) SyncUpdateVolumeSnapshotContents(vsrl []*VolumeSnapshotResource) error {
	for _, vsr := range vsrl {
		err := o.SyncUpdateVolumeSnapshotContent(vsr)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) AsyncUpdateVolumeSnapshotContents(vsrl []*VolumeSnapshotResource) error {
	for _, vsr := range vsrl {
		err := o.AsyncUpdateVolumeSnapshotContent(vsr)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) MonitorUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource) {
	readyToUse := false
	for !readyToUse {
		time.Sleep(time.Duration(5) * time.Second)
		vs := &snapshotv1beta1api.VolumeSnapshot{}
		_ = o.client.Get(context.Background(), k8sclient.ObjectKey{
			Namespace: o.dmNamespace,
			Name:      vsr.VolumeSnapshotName,
		}, vs)
		readyToUse = *vs.Status.ReadyToUse
	}
}

func (o *Operation) AsyncUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource) error {
	// get volumesnapshot
	vs := &snapshotv1beta1api.VolumeSnapshot{}
	err := o.client.Get(context.Background(), k8sclient.ObjectKey{
		Namespace: o.dmNamespace,
		Name:      vsr.VolumeSnapshotName,
	}, vs)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get volume snapshot %s", vsr.VolumeSnapshotName))
		return err
	}

	o.updateVscSnapRef(vsr, vs.UID)
	vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
	_ = o.client.Get(context.Background(), k8sclient.ObjectKey{
		Name: vsr.VolumeSnapshotContentName,
	}, vsc)
	var vsHandle = vsc.Status.SnapshotHandle
	var volHandle = vsc.Spec.Source.VolumeHandle
	patch := []byte(fmt.Sprintf(`{
		"spec":{
			"source":{
				"snapshotHandle": "%s",
				"volumeHandle": "%s"
				}
			}
		}`, *vsHandle, *volHandle))
	err = o.client.Patch(context.Background(), &snapshotv1beta1api.VolumeSnapshotContent{
		ObjectMeta: metav1.ObjectMeta{
			Name: vsr.VolumeSnapshotContentName,
		},
	}, k8sclient.RawPatch(types.MergePatchType, patch))
	if err != nil {
		o.logger.Error(err, "Failed to patch volume snapshot content")
		return err
	}
	return nil
}

func (o *Operation) SyncUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource) error {
	err := o.AsyncUpdateVolumeSnapshotContent(vsr)
	if err != nil {
		return err
	}
	o.MonitorUpdateVolumeSnapshotContent(vsr)
	return nil
}

func (o *Operation) updateVscSnapRef(vsr *VolumeSnapshotResource, uid types.UID) error {
	// get volumesnapshotcontent
	vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
	err := o.client.Get(context.Background(), k8sclient.ObjectKey{
		Name: vsr.VolumeSnapshotContentName,
	}, vsc)
	if err != nil {
		o.logger.Error(err, "Failed to get volume snapshot content")
		return err
	}
	vsc.Spec.VolumeSnapshotRef = core.ObjectReference{}
	vsc.Spec.VolumeSnapshotRef.Name = vsr.VolumeSnapshotName
	vsc.Spec.VolumeSnapshotRef.Namespace = o.dmNamespace
	vsc.Spec.VolumeSnapshotRef.UID = uid
	vsc.Spec.VolumeSnapshotRef.APIVersion = "snapshot.storage.k8s.io/v1beta1"
	vsc.Spec.VolumeSnapshotRef.Kind = "VolumeSnapshot"
	err = o.client.Update(context.TODO(), vsc)
	if err != nil {
		if errors.IsConflict(err) {
			o.UpdatePV(vsr.VolumeSnapshotContentName)
		} else {
			o.logger.Error(err, fmt.Sprintf("Failed to update volumesnapshotcontent %s to remove snapshot reference", vsr.VolumeSnapshotContentName))
			return err
		}
	}
	o.logger.Info(fmt.Sprintf("Update volumesnapshotcontent %s to remove snapshot reference", vsr.VolumeSnapshotContentName))
	return nil
}

func (o *Operation) GetVolumeSnapshotList(backupName string, ns string) (*snapshotv1beta1api.VolumeSnapshotList, error) {
	volumeSnapshotList := &snapshotv1beta1api.VolumeSnapshotList{}
	options := &k8sclient.ListOptions{}
	if backupName != "" {
		labels := map[string]string{
			config.VeleroBackupLabel: backupName,
		}
		options = &k8sclient.ListOptions{
			Namespace:     ns,
			LabelSelector: k8slabels.SelectorFromSet(labels),
		}
	} else {
		options = &k8sclient.ListOptions{
			Namespace: ns,
		}
	}
	var err = o.client.List(context.TODO(), volumeSnapshotList, options)
	return volumeSnapshotList, err
}

func (o *Operation) GetVolumeSnapshotResources(backupName string, backupNs string, tmpNs string) ([]*VolumeSnapshotResource, error) {
	origVolumeSnapshotList, err := o.GetVolumeSnapshotList(backupName, backupNs)
	if err != nil {
		return nil, err
	}
	var vsrl = make([]*VolumeSnapshotResource, len(origVolumeSnapshotList.Items))
	var i int
	var idMap = make(map[string]int)
	for _, vs := range origVolumeSnapshotList.Items {

		vsr := &VolumeSnapshotResource{
			VolumeSnapshotName:        vs.Name,
			VolumeSnapshotUID:         vs.UID,
			PersistentVolumeClaimName: *vs.Spec.Source.PersistentVolumeClaimName,
			VolumeSnapshotContentName: *vs.Status.BoundVolumeSnapshotContentName}
		vsrl[i] = vsr
		idMap[*vs.Status.BoundVolumeSnapshotContentName] = i
		i = i + 1
	}
	newVolumeSnapshotList, err := o.GetVolumeSnapshotList("", tmpNs)
	if err != nil {
		return nil, err
	}
	o.logger.Info("idMap: ", "value", idMap)
	for _, tmpVs := range newVolumeSnapshotList.Items {
		o.logger.Info("tmpVs: ", "value", tmpVs)
		index := idMap[*tmpVs.Spec.Source.VolumeSnapshotContentName]
		vsrl[index].VolumeSnapshotName = tmpVs.Name
		vsrl[index].VolumeSnapshotUID = tmpVs.UID
	}
	return vsrl, err
}
