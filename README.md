## Data-Mover

Data-Mover is an automation tool to export volumesnapshot data generated by Velero snapshot copy from local storage system to remote object storage system. It could also import the data back when there is any disaster with local storage system.

## Installation

git clone git@github.com:jibudata/data-mover.git

## Build CLI
```
make build-cli
./bin/data-mover-cli
```

## Usage

### CLI Example

#### 备份数据

```
go run main.go --action backup --backupName wp-backup-snap-76mxp-hzb2f --namespace wordpress
=== Step 0. Create temporay namespace + dm-wp-backup-snap-76mxp-hzb2f
=== Step 1. Create new volumesnapshot in temporary namespace
name: velero-mysql-pv-claim-q6jgv, uid: 532b6050-1bd7-4a6f-abfc-1a900bb52fc1, pvc: mysql-pv-claim, content_name: snapcontent-532b6050-1bd7-4a6f-abfc-1a900bb52fc1
Deleted volumesnapshot: velero-mysql-pv-claim-q6jgv in namesapce wordpress
Created volumesnapshot: velero-mysql-pv-claim-q6jgv in dm-wp-backup-snap-76mxp-hzb2f
name: velero-wp-pv-claim-p4lhl, uid: ada383d6-c23d-48fc-93fd-cad20f863cf4, pvc: wp-pv-claim, content_name: snapcontent-ada383d6-c23d-48fc-93fd-cad20f863cf4
Deleted volumesnapshot: velero-wp-pv-claim-p4lhl in namesapce wordpress
Created volumesnapshot: velero-wp-pv-claim-p4lhl in dm-wp-backup-snap-76mxp-hzb2f
=== Step 2. Update volumesnapshot content to new volumesnapshot in temporary namespace
Update volumesnapshotcontent snapcontent-532b6050-1bd7-4a6f-abfc-1a900bb52fc1 to remove snapshot reference
Update volumesnapshotcontent snapcontent-ada383d6-c23d-48fc-93fd-cad20f863cf4 to remove snapshot reference
=== Step 3. Create pvc reference to the new volumesnapshot in temporary namespace
Created pvc mysql-pv-claim in dm-wp-backup-snap-76mxp-hzb2f
Created pvc wp-pv-claim in dm-wp-backup-snap-76mxp-hzb2f
=== Step 4. Recreate pvc to reference pv created in step 3
Get pvc mysql-pv-claim and pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346
Patch pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346 with retain option
Deleted pvc mysql-pv-claim
Update pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346 to remove reference in dm-wp-backup-snap-76mxp-hzb2f
Update pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346 to remove reference in dm-wp-backup-snap-76mxp-hzb2f
Create pvc mysql-pv-claim in dm-wp-backup-snap-76mxp-hzb2f with pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346
Patch pv pvc-7fb33118-02a7-42db-9b18-2ba2a88c1346 with delete option
Get pvc wp-pv-claim and pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a
Patch pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a with retain option
Deleted pvc wp-pv-claim
Update pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a to remove reference in dm-wp-backup-snap-76mxp-hzb2f
Update pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a to remove reference in dm-wp-backup-snap-76mxp-hzb2f
Create pvc wp-pv-claim in dm-wp-backup-snap-76mxp-hzb2f with pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a
Patch pv pvc-297cb6ad-322b-4a9a-80a8-e51057d0e28a with delete option
=== Step 5. Create pod with pvc created in step 4
build stage pod wordpress-589f976cd5-vbj5z
build stage pod wordpress-mysql-d9b8d8884-9g4r5
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Pending
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Pending
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Pending
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Running
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Pending
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Running
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Pending
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Running
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Pending
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Running
pod stage-wordpress-589f976cd5-vbj5z-d4zg7 status Running
pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r status Running
=== Step 6. Invoke velero to backup the temporary namespace using file system copy
Get velero backup plan wp-backup-snap-76mxp-hzb2f
Created velero backup plan generate-backup-kql6f
```

#### 恢复数据

```
go run main.go --action restore --backupName wp-backup-snap-76mxp-hzb2f --namespace wordpress
=== Step 1. Get filesystem copy backup
generate-backup-kql6f
=== Step 2. Delete namespace
=== Step 3. Invoke velero to restore the temporary namespace to given namespace
Created velero restore plan generate-restore-ppdmp
=== Step 4. Delete pod in given namespace
Deleted pod stage-wordpress-589f976cd5-vbj5z-d4zg7
Deleted pod stage-wordpress-mysql-d9b8d8884-9g4r5-xzr8r
=== Step 5. Invoke velero to restore original namespace
Created velero restore plan generate-restore-tfqhz
```

### Use CR

Other backup solution can use CR for API level integration with data mover, below are CR details.

##### VeleroExport CR Spec

| Param              | Type                    | Supported value           | Description                                                  |
| ------------------ | ----------------------- | ------------------------- | ------------------------------------------------------------ |
| VeleroBackupRef    | *corev1.ObjectReference | name: xxx, namespace: xxx | Object reference of Velero backup which backup the namespaces using snapshot copy method |
| IncludedNamespaces | []string                | Any                       | Volumesnapshots in the namespaces will be exported           |
| DataSourceMapping  | map[string]string       | Any                       | DataSourceMapping is a map of pvc names to volumesnapshot names to be exported. |
| Policy             | ExportPolicy            | Retention: xx             | specify the veleroexport retention                           |

##### VeleroExport Status

| Status            | Description                                        |
| ----------------- | -------------------------------------------------- |
| StateInProgress   | Data export is in progress                         |
| StateFailed       | Data export is Failed                              |
| StateCompleted    | Data export is Completed                           |
| StateVeleroFailed | Data export is Failed due to velero backup failure |

##### VeleroImport CR Spec

| Param            | Type                    | Supported value           | Description                                                  |
| ---------------- | ----------------------- | ------------------------- | ------------------------------------------------------------ |
| VeleroBackupRef  | *corev1.ObjectReference | name: xxx, namespace: xxx | Object reference of Velero backup which backup the namespaces using snapshot copy method |
| NamespaceMapping | map[string]string       | any                       |                                                              |

##### VeleroImport Status

| Status            | Description                                        |
| ----------------- | -------------------------------------------------- |
| StateInProgress   | Data import is in progress                         |
| StateFailed       | Data import is failed                              |
| StateCompleted    | Data import is completed                           |
| StateVeleroFailed | Data import is failed due to velero backup failure |

