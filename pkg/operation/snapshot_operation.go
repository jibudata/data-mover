package operation

import (
	"context"
	"fmt"
	"strings"
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
	VolumeSnapshotName    string
	OrigVolumeSnapshotUID types.UID
	// persistentVolumeClaimName specifies the name of the PersistentVolumeClaim
	// object in the same namespace as the VolumeSnapshot object where the
	// snapshot should be dynamically taken from.
	PersistentVolumeClaimName string
	// volumeSnapshotContentName specifies the name of a pre-existing VolumeSnapshotContent
	// object.
	VolumeSnapshotContentName string
	NewVolumeSnapshotUID      types.UID
	PersistentVolumeName      string
}

// 1: get related VolumeSnapshotResource with backup and namespace
// 2. delete vs
// 3. create new vs in poc namespace
func (o *Operation) CreateVolumeSnapshots(backupName string, backupNs string, tgtNs string) ([]*VolumeSnapshotResource, error) {
	volumeSnapshotList, err := o.GetVolumeSnapshotList(backupName, backupNs)
	if err != nil {
		o.logger.Error(err, "Failed to get volume snapshot list")
		return nil, err
	}
	var vsrl = make([]*VolumeSnapshotResource, len(volumeSnapshotList.Items))
	var i int
	labels := map[string]string{
		config.VeleroBackupLabel: backupName,
	}
	for _, vs := range volumeSnapshotList.Items {
		vsr, err := o.CreateVolumeSnapshot(vs, labels, tgtNs)
		if err != nil {
			o.logger.Error(err, "Failed to create volume snapshot list", "for vs", vs.Name)
			return nil, err
		}
		vsrl[i] = vsr
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

func (o *Operation) CreateVolumeSnapshot(vs snapshotv1beta1api.VolumeSnapshot, labels map[string]string, namespace string) (*VolumeSnapshotResource, error) {
	volumeSnapshotName := vs.Name
	uid := vs.UID
	pvc := vs.Spec.Source.PersistentVolumeClaimName
	volumeSnapshotContentName := vs.Status.BoundVolumeSnapshotContentName
	o.logger.Info(fmt.Sprintf("name: %s, uid: %s, pvc: %s, content_name: %s", volumeSnapshotName, uid, *pvc, *volumeSnapshotContentName))
	// create new volumesnap shot
	newVs := &snapshotv1beta1api.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
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
	o.logger.Info(fmt.Sprintf("Created volumesnapshot: %s in %s", volumeSnapshotName, namespace))
	err := o.client.Create(context.TODO(), newVs)
	if err != nil {
		o.logger.Info(fmt.Sprintf("Failed to create volume snapshot in %s", namespace))
		return nil, err
	}
	// construct VolumeSnapshotResource
	vsr := &VolumeSnapshotResource{VolumeSnapshotName: volumeSnapshotName,
		OrigVolumeSnapshotUID:     vs.UID,
		PersistentVolumeClaimName: *pvc,
		VolumeSnapshotContentName: *volumeSnapshotContentName,
		NewVolumeSnapshotUID:      newVs.UID}
	return vsr, nil
}

// Update volumesnapshot content to new volumesnapshot in temporary namespace
func (o *Operation) SyncUpdateVolumeSnapshotContents(vsrl []*VolumeSnapshotResource, namespace string, recover bool) error {
	for _, vsr := range vsrl {
		err := o.SyncUpdateVolumeSnapshotContent(vsr, namespace, recover)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) AsyncUpdateVolumeSnapshotContents(vsrl []*VolumeSnapshotResource, namespace string, recover bool) error {
	for _, vsr := range vsrl {
		err := o.AsyncUpdateVolumeSnapshotContent(vsr, namespace, recover)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operation) MonitorUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource, namespace string) {
	readyToUse := false
	for !readyToUse {
		time.Sleep(time.Duration(5) * time.Second)
		vs := &snapshotv1beta1api.VolumeSnapshot{}
		_ = o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: namespace,
			Name:      vsr.VolumeSnapshotName,
		}, vs)
		readyToUse = *vs.Status.ReadyToUse
	}
}

func (o *Operation) IsVolumeSnapshotReady(vsrl []*VolumeSnapshotResource, namespace string) (bool, error) {

	for _, vsr := range vsrl {
		vs := &snapshotv1beta1api.VolumeSnapshot{}
		err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: namespace,
			Name:      vsr.VolumeSnapshotName,
		}, vs)
		if err != nil {
			o.logger.Error(err, "get volumesnapshot err")
			return false, err
		}

		if vs.Status == nil || vs.Status.ReadyToUse == nil {
			err = fmt.Errorf("vs status or ReadyToUse nil")
			o.logger.Error(err, "status not ready")
			return false, err
		}

		if !*vs.Status.ReadyToUse {
			return false, fmt.Errorf("volumesnapshot is not ready to use")
		}
	}
	return true, nil
}

func (o *Operation) AsyncUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource, namespace string, recover bool) error {
	var err error
	if recover {
		err = o.updateVscSnapRef(vsr, vsr.OrigVolumeSnapshotUID, namespace)
	} else {
		err = o.updateVscSnapRef(vsr, vsr.NewVolumeSnapshotUID, namespace)
	}
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to update volume snapshot content ref for %s", vsr.VolumeSnapshotName))
		return err
	}

	vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
	err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Name: vsr.VolumeSnapshotContentName,
	}, vsc)
	if err != nil {
		o.logger.Error(err, "Failed to get volume snapshot content")
		return err
	}

	if !recover {
		vsc.Spec.Source.SnapshotHandle = vsc.Status.SnapshotHandle
		vsc.Spec.Source.VolumeHandle = nil
		err = o.client.Update(context.TODO(), vsc)
		if err != nil {
			o.logger.Info("Failed to patch volume snapshot content", "error", err)
			return err
		}
	}

	return nil
}

func (o *Operation) SyncUpdateVolumeSnapshotContent(vsr *VolumeSnapshotResource, namespace string, recover bool) error {
	err := o.AsyncUpdateVolumeSnapshotContent(vsr, namespace, recover)
	if err != nil {
		return err
	}
	o.MonitorUpdateVolumeSnapshotContent(vsr, namespace)
	return nil
}

func (o *Operation) updateVscSnapRef(vsr *VolumeSnapshotResource, uid types.UID, namespace string) error {
	// get volumesnapshotcontent
	vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Name: vsr.VolumeSnapshotContentName,
	}, vsc)
	if err != nil {
		o.logger.Error(err, "Failed to get volume snapshot content")
		return err
	}

	vsc.Spec.VolumeSnapshotRef = core.ObjectReference{}
	vsc.Spec.VolumeSnapshotRef.Name = vsr.VolumeSnapshotName
	vsc.Spec.VolumeSnapshotRef.Namespace = namespace
	vsc.Spec.VolumeSnapshotRef.UID = uid
	vsc.Spec.VolumeSnapshotRef.APIVersion = "snapshot.storage.k8s.io/v1beta1"
	vsc.Spec.VolumeSnapshotRef.Kind = "VolumeSnapshot"
	err = o.client.Update(context.TODO(), vsc)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to update volumesnapshotcontent %s to remove snapshot reference", vsr.VolumeSnapshotContentName))
		if errors.IsConflict(err) {
			o.updateVscSnapRef(vsr, uid, namespace)
		} else {
			o.logger.Error(err, fmt.Sprintf("Failed to update volumesnapshotcontent %s to remove snapshot reference", vsr.VolumeSnapshotContentName))
			return err
		}
	}
	return nil
}

func (o *Operation) GetVolumeSnapshot(vsName string, ns string) (*snapshotv1beta1api.VolumeSnapshot, error) {

	volumeSnapshot := &snapshotv1beta1api.VolumeSnapshot{}
	var err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: ns,
		Name:      vsName,
	}, volumeSnapshot)
	if err != nil {
		return nil, err
	}
	return volumeSnapshot, nil
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
			OrigVolumeSnapshotUID:     vs.UID,
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

	for _, tmpVs := range newVolumeSnapshotList.Items {
		index := idMap[*tmpVs.Spec.Source.VolumeSnapshotContentName]
		vsrl[index].VolumeSnapshotName = tmpVs.Name
		vsrl[index].NewVolumeSnapshotUID = tmpVs.UID
	}
	return vsrl, err
}

func (o *Operation) GetVolumeSnapshotResourceList(vsrl string) []*VolumeSnapshotResource {
	if vsrl == "" {
		return nil
	}
	vsrList := strings.Split(vsrl, ";")
	var volumeSnapshotResourceList = make([]*VolumeSnapshotResource, len(vsrList))
	var i int = 0
	for _, vsr := range vsrList {
		volumeSnapshotResource := strings.Split(vsr, ",")
		volumeSnapshotResourceList[i] = &VolumeSnapshotResource{
			VolumeSnapshotName:        volumeSnapshotResource[0],
			OrigVolumeSnapshotUID:     types.UID(volumeSnapshotResource[1]),
			PersistentVolumeClaimName: volumeSnapshotResource[2],
			VolumeSnapshotContentName: volumeSnapshotResource[3],
			NewVolumeSnapshotUID:      types.UID(volumeSnapshotResource[4]),
			PersistentVolumeName:      volumeSnapshotResource[5],
		}
		i = i + 1
		// o.logger.Info("vsr", "VolumeSnapshotName", volumeSnapshotResourceList[i].VolumeSnapshotName, "VolumeSnapshotUID", volumeSnapshotResourceList[i].VolumeSnapshotUID, "PersistentVolumeClaimName", volumeSnapshotResourceList[i].PersistentVolumeClaimName, "VolumeSnapshotContentName", volumeSnapshotResourceList[i].VolumeSnapshotContentName)
	}
	return volumeSnapshotResourceList
}

func (o *Operation) DeleteVolumeSnapshots(vsrl []*VolumeSnapshotResource, namespace string) error {
	for _, vsr := range vsrl {
		volumeSnapshot := &snapshotv1beta1api.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      vsr.VolumeSnapshotName,
			},
		}
		err := o.client.Delete(context.TODO(), volumeSnapshot)
		if err != nil {
			return err
		}
	}
	return nil
}
