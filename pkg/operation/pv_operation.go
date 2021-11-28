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

func UpdatePV(client k8sclient.Client, PvName string, dmNamespace string) {
	pv := &core.PersistentVolume{}
	_ = client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: dmNamespace,
		Name:      PvName,
	}, pv)
	pv.Spec.ClaimRef = nil
	err := client.Update(context.TODO(), pv)
	if err != nil {
		if errors.IsConflict(err) {
			UpdatePV(client, PvName, dmNamespace)
		} else {
			fmt.Printf("Failed to update pv %s to remove reference in %s \n", PvName, dmNamespace)
			panic(err)
		}
	}
	fmt.Printf("Update pv %s to remove reference in %s \n", PvName, dmNamespace)
}

// 1. update pv to Retain
// 2. delete original pvc
// 3. update pv to be availble
// 4. create new pvc to reference pv
func CreatePvcWithVs(client k8sclient.Client, vsrl []VolumeSnapshotResource, ns *string, dmNamespace string) {
	for _, vsr := range vsrl {
		pvc := &core.PersistentVolumeClaim{}
		err := client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: *ns,
			Name:      vsr.PersistentVolumeClaimName,
		}, pvc)
		if err != nil {
			fmt.Printf("Failed to get pvc in %s \n", *ns)
			panic(err)
		}

		var apiGroup = "snapshot.storage.k8s.io"
		newPvc := &core.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dmNamespace,
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
		err = client.Create(context.Background(), newPvc)
		if err != nil {
			fmt.Printf("Failed to create pvc in %s", dmNamespace)
			panic(err)
		}
		fmt.Printf("Created pvc %s in %s \n", vsr.PersistentVolumeClaimName, dmNamespace)
	}
}

// Create pod with pvc
func CreatePvcWithPv(client k8sclient.Client, vsrl []VolumeSnapshotResource, ns *string, dmNamespace string) {
	for _, vsr := range vsrl {
		pvc := &core.PersistentVolumeClaim{}
		err := client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: dmNamespace,
			Name:      vsr.PersistentVolumeClaimName,
		}, pvc)
		if err != nil {
			fmt.Printf("Failed to get pvc in %s \n", dmNamespace)
			panic(err)
		}
		if pvc.Spec.VolumeName == "" {
			time.Sleep(time.Duration(5) * time.Second)
			err := client.Get(context.TODO(), k8sclient.ObjectKey{
				Namespace: dmNamespace,
				Name:      vsr.PersistentVolumeClaimName,
			}, pvc)
			if err != nil {
				fmt.Printf("Failed to get pvc in %s \n", dmNamespace)
				panic(err)
			}
		}
		pvName := pvc.Spec.VolumeName
		fmt.Printf("Get pvc %s and pv %s\n", vsr.PersistentVolumeClaimName, pvName)
		// patch the pv to Retain
		patch := []byte(`{"spec":{"persistentVolumeReclaimPolicy": "Retain"}}`)
		err = client.Patch(context.Background(), &core.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dmNamespace,
				Name:      pvName,
			},
		}, k8sclient.RawPatch(types.MergePatchType, patch))
		if err != nil {
			fmt.Println("Failed to patch pv to retain")
			panic(err)
		}
		fmt.Printf("Patch pv %s with retain option \n", pvName)
		// delete pvc
		err = client.Delete(context.Background(), pvc)
		if err != nil {
			fmt.Printf("Failed to delete pvc %s\n", pvc.Name)
			panic(err)
		}
		fmt.Printf("Deleted pvc %s \n", pvc.Name)
		// update pv to remove reference
		UpdatePV(client, pvName, dmNamespace)

		// create pvc with volume
		newPvc := &core.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dmNamespace,
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
		err = client.Create(context.Background(), newPvc)
		if err != nil {
			fmt.Printf("Failed to create pvc in %s", dmNamespace)
			panic(err)
		}
		fmt.Printf("Create pvc %s in %s with pv %s \n", vsr.PersistentVolumeClaimName, dmNamespace, pvName)

		// patch the pv to Delete
		patch = []byte(`{"spec": {"persistentVolumeReclaimPolicy": "Delete"}}`)
		err = client.Patch(context.Background(), &core.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dmNamespace,
				Name:      pvName,
			},
		}, k8sclient.RawPatch(types.MergePatchType, patch))
		if err != nil {
			fmt.Printf("Failed to patch pv %s with delete option \n", pvName)
			panic(err)
		}
		fmt.Printf("Patch pv %s with delete option \n", pvName)
	}
}
