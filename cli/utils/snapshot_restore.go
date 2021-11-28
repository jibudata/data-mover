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

func RestoreManager(client k8sclient.Client, backupName *string, ns *string) {
	dmNamespace := config.TempNamespace + "-" + *backupName
	fmt.Println("=== Step 1. Get filesystem copy backup")
	// Call velero to backup namespace using filesystem copy
	FcBpName := operation.GetVeleroBackup(client, *backupName)
	fmt.Println(FcBpName)
	fmt.Println("=== Step 2. Delete original namespace")
	operation.DeleteNamespace(client, *ns)
	fmt.Println("=== Step 3. Invoke velero to restore the temporary namespace to given namespace")
	operation.RestoreNamespace(client, FcBpName, dmNamespace, *ns)
	fmt.Println("=== Step 4. Delete pod in given namespace")
	operation.DeletePod(client, *ns)
	fmt.Println("=== Step 5. Invoke velero to restore original namespace")
	operation.RestoreNamespace(client, *backupName, *ns, *ns)
}
