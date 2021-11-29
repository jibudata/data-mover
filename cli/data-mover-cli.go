package main

import (
	"flag"
	"fmt"
	"os"

	dmUtil "github.com/jibudata/data-mover/cli/utils"
	snapshotv1beta1api "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	ActionBackup  = "backup"
	ActionRestore = "restore"
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

	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
	if *action == ActionBackup {
		dmUtil.BackupManager(client, backupName, ns)
	}
	if *action == ActionRestore {
		dmUtil.RestoreManager(client, backupName, ns)
	}
}
