package main

import (
	"flag"
	"os"

	dmUtil "github.com/jibudata/data-mover/cli/utils"
	operation "github.com/jibudata/data-mover/pkg/operation"
	snapshotv1beta1api "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	velero "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	core "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	logger := ctrl.Log.WithName("DataMover").WithName("Main")

	test := flag.String("test", "", "test")

	backupName := flag.String("backupName", "", "backup name")
	ns := flag.String("namespace", "", "namespace name")
	action := flag.String("action", "", "backup or restore")
	flag.Parse()
	if *backupName == "" && *test == "" {
		logger.Info("You must specify the velero backup name")
		os.Exit(0)
	}
	if *ns == "" && *test == "" {
		logger.Info("You must specify the namespace name")
		os.Exit(0)
	}
	if *action == "" && *test == "" {
		logger.Info("You must specify the action name - backup or restore")
		os.Exit(0)
	}

	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	var scheme *runtime.Scheme = runtime.NewScheme()
	if err := snapshotv1beta1api.AddToScheme(scheme); err != nil {
		logger.Info("unable to register snapshotv1beta1api to src scheme")
		os.Exit(1)
	}
	if err := core.AddToScheme(scheme); err != nil {
		logger.Info("unable to register core to scheme")
		os.Exit(1)
	}
	if err := velero.AddToScheme(scheme); err != nil {
		logger.Info("unable to register core to scheme")
		os.Exit(1)
	}

	client, err := k8sclient.New(config.GetConfigOrDie(), k8sclient.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}
	if *test != "" {
		handler := operation.NewOperation(logger, client)
		handler.GetVeleroBackupUID(client, types.UID("60d6f8fd-e686-4f2a-9e73-e1a4a4b4c69c"))
	} else {
		if *action == ActionBackup {
			dmUtil.BackupManager(client, backupName, ns)
		}
		if *action == ActionRestore {
			dmUtil.RestoreManager(client, backupName, ns)
		}
	}

}
