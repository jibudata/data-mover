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
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func BackupManager(client k8sclient.Client, backupName *string, ns *string) {
	dmNamespace := config.TempNamespace + "-" + *backupName
	fmt.Printf("=== Step 0. Create temporay namespace + %s\n", dmNamespace)
	operation.CreateNamespace(client, dmNamespace)
	fmt.Println("=== Step 1. Create new volumesnapshot in temporary namespace")
	var vsrl = operation.CreateVolumeSnapshot(client, backupName, ns, dmNamespace)
	fmt.Println("=== Step 2. Update volumesnapshot content to new volumesnapshot in temporary namespace")
	operation.UpdateVolumeSnapshotContent(client, vsrl, dmNamespace)
	fmt.Println("=== Step 3. Create pvc reference to the new volumesnapshot in temporary namespace")
	operation.CreatePvcWithVs(client, vsrl, ns, dmNamespace)
	fmt.Println("=== Step 4. Recreate pvc to reference pv created in step 3")
	operation.CreatePvcWithPv(client, vsrl, ns, dmNamespace)
	fmt.Println("=== Step 5. Create pod with pvc created in step 4")
	operation.BuildStagePod(client, *ns, dmNamespace)
	fmt.Println("=== Step 6. Invoke velero to backup the temporary namespace using file system copy")
	_ = operation.BackupNamespaceFc(client, *backupName, dmNamespace)
}
