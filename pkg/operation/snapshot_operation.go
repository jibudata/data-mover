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
func CreateVolumeSnapshot(client k8sclient.Client, backupName *string, ns *string, dmNamespace string) []VolumeSnapshotResource {
	volumeSnapshotList := &snapshotv1beta1api.VolumeSnapshotList{}
	labels := map[string]string{
		config.VeleroBackupLabel: *backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     *ns,
		LabelSelector: k8slabels.SelectorFromSet(labels),
	}
	var err = client.List(context.TODO(), volumeSnapshotList, options)
	if err != nil {
		fmt.Println("Failed to get volume snapshot list")
		panic(err)
	}
	var vsrl = make([]VolumeSnapshotResource, len(volumeSnapshotList.Items))
	var newVs = make([]snapshotv1beta1api.VolumeSnapshot, len(volumeSnapshotList.Items))
	var i int
	for _, vs := range volumeSnapshotList.Items {
		volumeSnapshotName := vs.Name
		uid := vs.UID
		pvc := vs.Spec.Source.PersistentVolumeClaimName
		volumeSnapshotContentName := vs.Status.BoundVolumeSnapshotContentName
		fmt.Printf("name: %s, uid: %s, pvc: %s, content_name: %s \n", volumeSnapshotName, uid, *pvc, *volumeSnapshotContentName)
		// delete volumesnap shot
		err = client.Delete(context.TODO(), &vs)
		if err != nil {
			fmt.Println("Failed to delete volume snapshot")
			panic(err)
		}
		fmt.Printf("Deleted volumesnapshot: %s in namesapce %s \n", volumeSnapshotName, *ns)
		time.Sleep(time.Duration(2) * time.Second)
		// create new volumesnap shot
		newVs[i] = snapshotv1beta1api.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dmNamespace,
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
		fmt.Printf("Created volumesnapshot: %s in %s\n", volumeSnapshotName, dmNamespace)
		err = client.Create(context.Background(), &newVs[i])
		if err != nil {
			fmt.Printf("Failed to create volume snapshot in %s\n", dmNamespace)
			panic(err)
		}
		// construct VolumeSnapshotResource
		vsr := VolumeSnapshotResource{VolumeSnapshotName: volumeSnapshotName,
			VolumeSnapshotUID:         newVs[i].UID,
			PersistentVolumeClaimName: *pvc,
			VolumeSnapshotContentName: *volumeSnapshotContentName}
		vsrl[i] = vsr
		i++
	}
	return vsrl
}

// Update volumesnapshot content to new volumesnapshot in temporary namespace
func UpdateVolumeSnapshotContent(client k8sclient.Client, vsrl []VolumeSnapshotResource, dmNamespace string) {
	for _, vsr := range vsrl {
		// get volumesnapshot
		vs := &snapshotv1beta1api.VolumeSnapshot{}
		err := client.Get(context.Background(), k8sclient.ObjectKey{
			Namespace: dmNamespace,
			Name:      vsr.VolumeSnapshotName,
		}, vs)
		if err != nil {
			fmt.Printf("Failed to get volume snapshot %s\n", vsr.VolumeSnapshotName)
			panic(err)
		}

		updateVscSnapRef(client, vsr, vs.UID, dmNamespace)
		vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
		_ = client.Get(context.Background(), k8sclient.ObjectKey{
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
		err = client.Patch(context.Background(), &snapshotv1beta1api.VolumeSnapshotContent{
			ObjectMeta: metav1.ObjectMeta{
				Name: vsr.VolumeSnapshotContentName,
			},
		}, k8sclient.RawPatch(types.MergePatchType, patch))
		if err != nil {
			fmt.Println("Failed to patch volume snapshot content")
			panic(err)
		}
		readyToUse := false
		for !readyToUse {
			time.Sleep(time.Duration(5) * time.Second)
			vs = &snapshotv1beta1api.VolumeSnapshot{}
			_ = client.Get(context.Background(), k8sclient.ObjectKey{
				Namespace: dmNamespace,
				Name:      vsr.VolumeSnapshotName,
			}, vs)
			readyToUse = *vs.Status.ReadyToUse
		}
	}
}

func updateVscSnapRef(client k8sclient.Client, vsr VolumeSnapshotResource, uid types.UID, dmNamespace string) {
	// get volumesnapshotcontent
	vsc := &snapshotv1beta1api.VolumeSnapshotContent{}
	err := client.Get(context.Background(), k8sclient.ObjectKey{
		Name: vsr.VolumeSnapshotContentName,
	}, vsc)
	if err != nil {
		fmt.Println("Failed to get volume snapshot content")
		panic(err)
	}
	vsc.Spec.VolumeSnapshotRef = core.ObjectReference{}
	vsc.Spec.VolumeSnapshotRef.Name = vsr.VolumeSnapshotName
	vsc.Spec.VolumeSnapshotRef.Namespace = dmNamespace
	vsc.Spec.VolumeSnapshotRef.UID = uid
	vsc.Spec.VolumeSnapshotRef.APIVersion = "snapshot.storage.k8s.io/v1beta1"
	vsc.Spec.VolumeSnapshotRef.Kind = "VolumeSnapshot"
	err = client.Update(context.TODO(), vsc)
	if err != nil {
		if errors.IsConflict(err) {
			UpdatePV(client, vsr.VolumeSnapshotContentName, dmNamespace)
		} else {
			fmt.Printf("Failed to update volumesnapshotcontent %s to remove snapshot reference \n", vsr.VolumeSnapshotContentName)
			panic(err)
		}
	}
	fmt.Printf("Update volumesnapshotcontent %s to remove snapshot reference\n", vsr.VolumeSnapshotContentName)
}
