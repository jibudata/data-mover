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
	"fmt"

	config "github.com/jibudata/data-mover/pkg/config"
	operation "github.com/jibudata/data-mover/pkg/operation"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func RestoreManager(client k8sclient.Client, backupName *string, ns *string) {
	logger := ctrl.Log.WithName("DataMover").WithName("RestoreManager")
	dmNamespace := config.TempNamespace + "-" + *backupName
	handler := operation.NewOperation(logger, client)

	fmt.Println("=== Step 1. Get filesystem copy backup")
	// Call velero to backup namespace using filesystem copy
	backup, err := handler.GetVeleroBackup(*backupName, config.VeleroNamespace)
	if err != nil || backup == nil {
		panic(err)
	}
	fmt.Println(backup.Name)
	fmt.Println("=== Step 2. Delete original namespace")
	err = handler.SyncDeleteNamespace(*ns)
	if err != nil {
		panic(err)
	}
	fmt.Println("=== Step 3. Invoke velero to restore the temporary namespace to given namespace")
	_, err = handler.SyncRestoreNamespace(backup.Name, config.VeleroNamespace, dmNamespace, *ns)
	if err != nil {
		panic(err)
	}
	fmt.Println("=== Step 4. Delete pod in given namespace")
	err = handler.SyncDeleteStagePod(*ns)
	if err != nil {
		panic(err)
	}
	fmt.Println("=== Step 5. Invoke velero to restore original namespace")
	_, err = handler.SyncRestoreNamespace(*backupName, config.VeleroNamespace, *ns, *ns)
	if err != nil {
		panic(err)
	}
}
