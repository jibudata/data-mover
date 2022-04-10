package config

const (
	TempNamespace            = "dm"
	VeleroBackupNamePrefix   = "ve-"
	VeleroRestoreNamePrefix  = "vi-"
	StagePodImage            = "registry.cn-shanghai.aliyuncs.com/jibudata/velero-restic-restore-helper:v1.6.3"
	StagePodVersion          = "velero-restic-restore-helper:v1.7.0"
	VeleroNamespace          = "qiming-backend"
	VeleroBackupLabel        = "velero.io/backup-name"
	VeleroStorageLabel       = "velero.io/storage-location"
	VeleroSrcClusterGitAnn   = "velero.io/source-cluster-k8s-gitversion"
	VeleroK8sMajorVerAnn     = "velero.io/source-cluster-k8s-major-version"
	VeleroK8sMinorVerAnn     = "velero.io/source-cluster-k8s-minor-version"
	SnapshotExportBackupName = "snapshot-export-velero-backup-name"
	// DataExportName           = "data-export-name"
	TempNamespacePrefix = "dm-"
	PodNamePrefix       = "data-mover"
)

var (
	False = false
	True  = true
)
