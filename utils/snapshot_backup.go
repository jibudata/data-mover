/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	snapshotv1beta1api "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

// StagePod - wrapper for stage pod, allowing to compare  two stage pods for equality
type StagePod struct {
	core.Pod
}

// StagePodList - a list of stage pods, with built-in stage pod deduplication
type StagePodList []StagePod

const TempNamespace string = "dm"
const (
	GenerateBackupName  = "generate-backup-"
	GenerateRestoreName = "generate-restore-"
	stagePodImage       = "registry.cn-shanghai.aliyuncs.com/jibudata/velero-restic-restore-helper:v1.6.3"
)
const (
	VeleroNamespace        = "qiming-backend"
	VeleroBackupLabel      = "velero.io/backup-name"
	VeleroStorageLabel     = "velero.io/storage-location"
	VeleroSrcClusterGitAnn = "velero.io/source-cluster-k8s-gitversion"
	VeleroK8sMajorVerAnn   = "velero.io/source-cluster-k8s-major-version"
	VeleroK8sMinorVerAnn   = "velero.io/source-cluster-k8s-minor-version"
)
const (
	memory        = "memory"
	cpu           = "cpu"
	defaultMemory = "128Mi"
	defaultCPU    = "100m"
)

var dmNamespace string

var f = false
var t = true

func BackupManager(client k8sclient.Client, backupName *string, ns *string) {
	dmNamespace = TempNamespace + "-" + *backupName
	fmt.Printf("=== Step 0. Create temporay namespace + %s\n", dmNamespace)
	createNamespace(client, dmNamespace)
	fmt.Println("=== Step 1. Create new volumesnapshot in temporary namespace")
	var vsrl = createVolumeSnapshot(client, backupName, ns)
	fmt.Println("=== Step 2. Update volumesnapshot content to new volumesnapshot in temporary namespace")
	updateVolumeSnapshotContent(client, vsrl)
	fmt.Println("=== Step 3. Create pvc reference to the new volumesnapshot in temporary namespace")
	createPvcWithVs(client, vsrl, ns)
	fmt.Println("=== Step 4. Recreate pvc to reference pv created in step 3")
	createPvcWithPv(client, vsrl, ns)
	fmt.Println("=== Step 5. Create pod with pvc created in step 4")
	buildStagePod(client, *ns)
	fmt.Println("=== Step 6. Invoke velero to backup the temporary namespace using file system copy")
	_ = backupNamespaceFc(client, *backupName)
}

// 1: get related VolumeSnapshotResource with backup and namespace
// 2. delete vs
// 3. create new vs in poc namespace
func createVolumeSnapshot(client k8sclient.Client, backupName *string, ns *string) []VolumeSnapshotResource {
	volumeSnapshotList := &snapshotv1beta1api.VolumeSnapshotList{}
	labels := map[string]string{
		VeleroBackupLabel: *backupName,
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
func updateVolumeSnapshotContent(client k8sclient.Client, vsrl []VolumeSnapshotResource) {
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

		updateVscSnapRef(client, vsr, vs.UID)
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

func updateVscSnapRef(client k8sclient.Client, vsr VolumeSnapshotResource, uid types.UID) {
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
			updateObject(client, vsr.VolumeSnapshotContentName)
		} else {
			fmt.Printf("Failed to update volumesnapshotcontent %s to remove snapshot reference \n", vsr.VolumeSnapshotContentName)
			panic(err)
		}
	}
	fmt.Printf("Update volumesnapshotcontent %s to remove snapshot reference\n", vsr.VolumeSnapshotContentName)
}

// 1. update pv to Retain
// 2. delete original pvc
// 3. update pv to be availble
// 4. create new pvc to reference pv
func createPvcWithVs(client k8sclient.Client, vsrl []VolumeSnapshotResource, ns *string) {
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
func createPvcWithPv(client k8sclient.Client, vsrl []VolumeSnapshotResource, ns *string) {
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
		updateObject(client, pvName)

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

func updateObject(client k8sclient.Client, objectName string) {
	pv := &core.PersistentVolume{}
	_ = client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: dmNamespace,
		Name:      objectName,
	}, pv)
	pv.Spec.ClaimRef = nil
	err := client.Update(context.TODO(), pv)
	if err != nil {
		if errors.IsConflict(err) {
			updateObject(client, objectName)
		} else {
			fmt.Printf("Failed to update pv %s to remove reference in %s \n", objectName, dmNamespace)
			panic(err)
		}
	}
	fmt.Printf("Update pv %s to remove reference in %s \n", objectName, dmNamespace)
}

// backup poc namespace using velero
func buildStagePod(client k8sclient.Client, backupNs string) {
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: backupNs,
	}
	_ = client.List(context.Background(), podList, options)
	stagePods := BuildStagePods(&podList.Items, stagePodImage, dmNamespace)
	for _, stagePod := range stagePods {
		err := client.Create(context.Background(), &stagePod.Pod)
		if err != nil {
			fmt.Printf("Failed to crate pod %s\n", stagePod.Pod.Name)
			panic(err)
		}
	}
	running := false
	options = &k8sclient.ListOptions{
		Namespace: dmNamespace,
	}
	for !running {
		time.Sleep(time.Duration(5) * time.Second)
		podList = &core.PodList{}
		_ = client.List(context.Background(), podList, options)
		running = true
		for _, pod := range podList.Items {
			fmt.Printf("pod %s status %s\n", pod.Name, pod.Status.Phase)
			if pod.Status.Phase != "Running" {
				running = false
			}
		}
	}
}

// Call velero to backup namespace using filesystem copy
func backupNamespaceFc(client k8sclient.Client, backupName string) string {
	// get velero backup plan
	bp := &velero.Backup{}
	err := client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: VeleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		fmt.Printf("Failed to get velero backup plan %s \n", backupName)
		panic(err)
	}
	fmt.Printf("Get velero backup plan %s \n", backupName)
	labels := map[string]string{
		VeleroStorageLabel: bp.Labels[VeleroStorageLabel],
		VeleroBackupLabel:  backupName,
	}
	annotation := map[string]string{
		VeleroSrcClusterGitAnn: bp.Annotations[VeleroSrcClusterGitAnn],
		VeleroK8sMajorVerAnn:   bp.Annotations[VeleroK8sMajorVerAnn],
		VeleroK8sMinorVerAnn:   bp.Annotations[VeleroK8sMinorVerAnn],
	}
	var newBp *velero.Backup = &velero.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Labels:       labels,
			GenerateName: GenerateBackupName,
			Namespace:    bp.Namespace,
			Annotations:  annotation,
		},
		Spec: velero.BackupSpec{
			// IncludeClusterResources: includeClusterResources,
			StorageLocation:    bp.Spec.StorageLocation,
			TTL:                bp.Spec.TTL,
			IncludedNamespaces: []string{dmNamespace},
			Hooks: velero.BackupHooks{
				Resources: []velero.BackupResourceHookSpec{},
			},
			SnapshotVolumes:        &(f),
			DefaultVolumesToRestic: &(t),
		},
	}
	err = client.Create(context.TODO(), newBp)
	if err != nil {
		fmt.Printf("Failed to create velero backup plan %s \n", newBp.Name)
		panic(err)
	}
	fmt.Printf("Created velero backup plan %s \n", newBp.Name)
	// get velero backup plan
	getBackup(client, newBp.Name)
	return newBp.Name
}

func getBackup(client k8sclient.Client, backupName string) {
	bp := &velero.Backup{}
	err := client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: VeleroNamespace,
		Name:      backupName,
	}, bp)
	if err != nil {
		fmt.Printf("Failed to get velero backup plan %s \n", backupName)
		panic(err)
	}
	if bp.Status.Phase != velero.BackupPhaseCompleted {
		time.Sleep(time.Duration(5) * time.Second)
		getBackup(client, backupName)
	}
}

func createNamespace(client k8sclient.Client, ns string) {

	namespace := core.Namespace{}
	err := client.Get(context.TODO(), k8sclient.ObjectKey{
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
			err = client.Create(context.TODO(), &namespace)
			if err != nil {
				fmt.Printf("Failed to delete namespace %s \n", ns)
				panic(err)
			}
			return

		} else {
			fmt.Printf("Failed to get namespace %s \n", ns)
			panic(err)
		}
	}
	err = client.Delete(context.TODO(), &namespace)
	if err != nil {
		fmt.Printf("Failed to delete namespace %s \n", ns)
		panic(err)
	}
	err = nil
	for err == nil {
		namespace := core.Namespace{}
		err = client.Get(context.TODO(), k8sclient.ObjectKey{
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
	err = client.Create(context.TODO(), &namespace)
	if err != nil {
		fmt.Printf("Failed to create namespace %s \n", ns)
		panic(err)
	}
}

func deleteNamespace(client k8sclient.Client, ns string) {
	namespace := core.Namespace{}
	err := client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: ns,
		Name:      ns,
	}, &namespace)
	if err != nil {
		fmt.Printf("Failed to get namespace %s \n", ns)
		panic(err)
	}
	client.Delete(context.TODO(), &namespace)
	err = nil
	for err == nil {
		time.Sleep(time.Duration(15) * time.Second)
		namespace := core.Namespace{}
		err = client.Get(context.TODO(), k8sclient.ObjectKey{
			Namespace: ns,
			Name:      ns,
		}, &namespace)
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

// BuildStagePods - creates a list of stage pods from a list of pods
func BuildStagePods(list *[]core.Pod, stagePodImage string, ns string) StagePodList {

	stagePods := StagePodList{}
	for _, pod := range *list {
		volumes := []core.Volume{}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			volumes = append(volumes, volume)
		}
		if len(volumes) == 0 {
			continue
		}
		podKey := k8sclient.ObjectKey{
			Name:      pod.GetName(),
			Namespace: ns,
		}
		fmt.Printf("build stage pod %s\n", pod.Name)
		stagePod := buildStagePodFromPod(podKey, &pod, volumes, stagePodImage)
		if stagePod != nil {
			stagePods.merge(*stagePod)
		}
	}
	return stagePods
}

// Build a stage pod based on existing pod.
func buildStagePodFromPod(ref k8sclient.ObjectKey, pod *core.Pod, pvcVolumes []core.Volume, stagePodImage string) *StagePod {

	// Base pod.
	newPod := &StagePod{
		Pod: core.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    dmNamespace,
				GenerateName: truncateName("stage-"+ref.Name) + "-",
			},
			Spec: core.PodSpec{
				Containers: []core.Container{},
				NodeName:   pod.Spec.NodeName,
				Volumes:    pvcVolumes,
			},
		},
	}

	inVolumes := func(mount core.VolumeMount) bool {
		for _, volume := range pvcVolumes {
			if volume.Name == mount.Name {
				return true
			}
		}
		return false
	}
	podMemory, _ := resource.ParseQuantity(defaultMemory)
	podCPU, _ := resource.ParseQuantity(defaultCPU)
	// Add containers.
	for i, container := range pod.Spec.Containers {
		volumeMounts := []core.VolumeMount{}
		for _, mount := range container.VolumeMounts {
			if inVolumes(mount) {
				volumeMounts = append(volumeMounts, mount)
			}
		}
		stageContainer := core.Container{
			Name:            "sleep-" + strconv.Itoa(i),
			Image:           stagePodImage,
			ImagePullPolicy: core.PullIfNotPresent,
			Command:         []string{"sleep"},
			Args:            []string{"infinity"},
			VolumeMounts:    volumeMounts,
			Resources: core.ResourceRequirements{
				Requests: core.ResourceList{
					memory: podMemory,
					cpu:    podCPU,
				},
				Limits: core.ResourceList{
					memory: podMemory,
					cpu:    podCPU,
				},
			},
		}

		newPod.Spec.Containers = append(newPod.Spec.Containers, stageContainer)
	}

	return newPod
}

func (p StagePod) volumesContained(pod StagePod) bool {
	if p.Namespace != pod.Namespace {
		return false
	}
	for _, volume := range p.Spec.Volumes {
		found := false
		for _, targetVolume := range pod.Spec.Volumes {
			if reflect.DeepEqual(volume.VolumeSource, targetVolume.VolumeSource) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (l *StagePodList) contains(pod StagePod) bool {
	for _, srcPod := range *l {
		if pod.volumesContained(srcPod) {
			return true
		}
	}

	return false
}

func (l *StagePodList) merge(list ...StagePod) {
	for _, pod := range list {
		if !l.contains(pod) {
			*l = append(*l, pod)
		}
	}
}

func truncateName(name string) string {
	r := regexp.MustCompile(`(-+)`)
	name = r.ReplaceAllString(name, "-")
	name = strings.TrimRight(name, "-")
	if len(name) > 57 {
		name = name[:57]
	}
	return name
}
