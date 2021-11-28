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

func CreateNamespace(client k8sclient.Client, ns string) {

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

func DeleteNamespace(client k8sclient.Client, ns string) {
	namespace := core.Namespace{}
	err := client.Get(context.TODO(), k8sclient.ObjectKey{
		Name: ns,
	}, &namespace)
	if err != nil {
		fmt.Printf("Failed to get namespace %s \n", ns)
		// If namespace not exist, skip
		if serr, ok := err.(*errors.StatusError); ok {
			if serr.Status().Reason == "NotFound" {
				fmt.Printf("Skip deleting non-existing namespace %s \n", ns)
				return
			}
		}
		// TBD: panic to error handling
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
func RestoreNamespace(client k8sclient.Client, backupName string, srcNamespace string, tgtNamespace string) string {
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
			Namespace: config.VeleroNamespace,
		}
		client.Get(context.Background(), key, restore)
		time.Sleep(time.Duration(5) * time.Second)
		status = string(restore.Status.Phase)
	}
	return restore.Name
}
