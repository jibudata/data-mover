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
	"time"

	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func RestoreManager(client k8sclient.Client, backupName *string, ns *string) {
	dmNamespace = TempNamespace + "-" + *backupName
	fmt.Println("=== Step 1. Get filesystem copy backup")
	FcBpName := getFcBackup(client, *backupName)
	fmt.Println(FcBpName)
	fmt.Println("=== Step 2. Delete namespace")
	deleteOrigNamespace(client, *ns)
	fmt.Println("=== Step 3. Invoke velero to restore the temporary namespace to given namespace")
	restoreNamespace(client, FcBpName, dmNamespace, *ns)
	fmt.Println("=== Step 4. Delete pod in given namespace")
	deletePod(client, *ns)
	fmt.Println("=== Step 5. Invoke velero to restore original namespace")
	restoreNamespace(client, *backupName, *ns, *ns)
}

func deleteOrigNamespace(client k8sclient.Client, ns string) {
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

// delete pod
func deletePod(client k8sclient.Client, ns string) {
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: ns,
	}
	err := client.List(context.Background(), podList, options)
	if err != nil {
		fmt.Printf("Failed to get pod list in namespace %s\n", dmNamespace)
		panic(err)
	}
	for _, pod := range podList.Items {
		var name = pod.Name
		err = client.Delete(context.Background(), &pod)
		if err != nil {
			fmt.Printf("Failed to delete pvc %s\n", pod.Name)
			panic(err)
		}
		fmt.Printf("Deleted pod %s \n", name)
	}
	var running = true
	for running {
		time.Sleep(time.Duration(5) * time.Second)
		podList = &core.PodList{}
		options = &k8sclient.ListOptions{
			Namespace: ns,
		}
		_ = client.List(context.Background(), podList, options)
		if len(podList.Items) == 0 {
			running = false
		}
	}
}

// Call velero to backup namespace using filesystem copy
func getFcBackup(client k8sclient.Client, backupName string) string {
	backups := &velero.BackupList{}
	labels := map[string]string{
		VeleroBackupLabel: backupName,
	}
	options := &k8sclient.ListOptions{
		Namespace:     VeleroNamespace,
		LabelSelector: k8slabels.SelectorFromSet(labels),
	}
	err := client.List(context.TODO(), backups, options)
	if err != nil {
		fmt.Printf("Failed to get velero backup plan %s \n", backupName)
		panic(err)
	}
	bp := backups.Items[0]
	return bp.Name
}

// Restore original namespace using velero
func restoreNamespace(client k8sclient.Client, backupName string, srcNamespace string, tgtNamespace string) string {
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
			GenerateName: GenerateRestoreName,
			Namespace:    VeleroNamespace,
		},
		Spec: velero.RestoreSpec{
			BackupName:        backupName,
			RestorePVs:        &(t),
			ExcludedResources: excludedResources,
			NamespaceMapping:  nsMapping,
		},
	}
	err := client.Create(context.TODO(), restore)
	if err != nil {
		fmt.Printf("Failed to create velero restore plan %s \n", restore.Name)
		panic(err)
	}
	fmt.Printf("Created velero restore plan %s \n", restore.Name)

	var restoreName string = restore.Name
	status := string(restore.Status.Phase)
	for status != "Completed" {
		restore = &velero.Restore{}
		key := k8sclient.ObjectKey{
			Name:      restoreName,
			Namespace: VeleroNamespace,
		}
		client.Get(context.Background(), key, restore)
		time.Sleep(time.Duration(5) * time.Second)
		status = string(restore.Status.Phase)
	}
	return restore.Name
}
