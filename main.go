package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jibudata/snapshot-export/utils"
	snapshotv1beta1api "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {

	backupName := flag.String("backupName", "", "backup name")
	ns := flag.String("namespace", "", "namespace name")
	action := flag.String("action", "", "backup or restore")
	flag.Parse()
	if *backupName == "" {
		fmt.Println("You must specify the deployment name.")
		os.Exit(0)
	}
	if *ns == "" {
		fmt.Println("You must specify the namespace name.")
		os.Exit(0)
	}

	var scheme *runtime.Scheme = runtime.NewScheme()
	if err := snapshotv1beta1api.AddToScheme(scheme); err != nil {
		fmt.Println("unable to register snapshotv1beta1api to src scheme")
		os.Exit(1)
	}
	if err := core.AddToScheme(scheme); err != nil {
		fmt.Println("unable to register core to scheme")
		os.Exit(1)
	}
	if err := velero.AddToScheme(scheme); err != nil {
		fmt.Println("unable to register core to scheme")
		os.Exit(1)
	}

	client, err := k8sclient.New(config.GetConfigOrDie(), k8sclient.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}
	if *action == "backup" {
		fmt.Println("=== Step 0. Create temporay namespace")
		utils.createNamespace(client, utils.TempNamespace)
		fmt.Println("=== Step 1. Create new volumesnapshot in temporary namespace")
		var vsrl = utils.createVolumeSnapshot(client, backupName, ns)
		fmt.Println("=== Step 2. Update volumesnapshot content to new volumesnapshot in temporary namespace")
		utils.updateVolumeSnapshotContent(client, vsrl)
		fmt.Println("=== Step 3. Create pvc reference to the new volumesnapshot in temporary namespace")
		utils.createPvcWithVs(client, vsrl, ns)
		fmt.Println("=== Step 4. Recreate pvc to reference pv created in step 3")
		utils.createPvcWithPv(client, vsrl, ns)
		fmt.Println("=== Step 5. Create pod with pvc created in step 4")
		utils.buildStagePod(client, *ns)
		fmt.Println("=== Step 6. Invoke velero to backup the temporary namespace using file system copy")
		_ = utils.backupNamespaceFc(client, *backupName)
	} else {
		fmt.Println("=== Step 1. Get filesystem copy backup")
		FcBpName := utils.getFcBackup(client, *backupName)
		fmt.Println(FcBpName)
		fmt.Println("=== Step 2. Delete namespace")
		utils.deleteOrigNamespace(client, *ns)
		fmt.Println("=== Step 3. Invoke velero to restore the temporary namespace to given namespace")
		utils.restoreNamespace(client, FcBpName, utils.TempNamespace, *ns)
		fmt.Println("=== Step 4. Delete pod in given namespace")
		utils.deletePod(client, *ns)
		fmt.Println("=== Step 5. Invoke velero to restore original namespace")
		utils.restoreNamespace(client, *backupName, *ns, *ns)

	}

}
