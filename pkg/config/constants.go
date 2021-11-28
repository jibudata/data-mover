package config

const (
	TempNamespace          = "dm"
	GenerateBackupName     = "generate-backup-"
	GenerateRestoreName    = "generate-restore-"
	StagePodImage          = "registry.cn-shanghai.aliyuncs.com/jibudata/velero-restic-restore-helper:v1.6.3"
	VeleroNamespace        = "qiming-backend"
	VeleroBackupLabel      = "velero.io/backup-name"
	VeleroStorageLabel     = "velero.io/storage-location"
	VeleroSrcClusterGitAnn = "velero.io/source-cluster-k8s-gitversion"
	VeleroK8sMajorVerAnn   = "velero.io/source-cluster-k8s-major-version"
	VeleroK8sMinorVerAnn   = "velero.io/source-cluster-k8s-minor-version"
)

var (
	False = false
	True  = true
)
