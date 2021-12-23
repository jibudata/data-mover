#!/bin/bash

# Test end to end velero import functionality
# Assumption:
# - Already execute velero export, got the velero backup name, and namespace to be restored
# Basic workflow:
# - Parse input
# - Create VeleroImport CR
# - Monitor CR status

TEST_NAMESPACE="e2e-test"
TEST_VELEROIMPORT_YAML="/tmp/test_veleroimport.yaml"
VELERO_NAMESPACE="qiming-backend"
VELEROIMPORT_NAME="veleroimport-sample-$(date "+%Y%m%d%H%M%S")"
BACKUP_NAME=
RESTORE_NAMESPACE=

usage() {
    echo "./e2e-veleroimport-test.sh -b <snapshot velero backupName> -r <restore namespace>"
}

EXEC() {
    echo "$1"
    eval $1
}

prepare () {
    EXEC "rm $TEST_VELEROIMPORT_YAML"
    EXEC "kubectl create namespace $TEST_NAMESPACE"
}

cleanup () {
    echo "Cleanup .."
    EXEC "kubectl delete namespace $TEST_NAMESPACE"
    EXEC "rm -rf $TEST_VELEROIMPORT_YAML"
}

create_cr() {
    echo "Create CR $VELEROIMPORT_NAME"

    veleroBackupName=$BACKUP_NAME
    veleroBackupNamespace=$VELERO_NAMESPACE
    restoreNamespace=$RESTORE_NAMESPACE
    pvcNames="[]"

    echo "Create CR yaml file $TEST_VELEROIMPORT_YAML"
    cat << TEMPLATE >> $TEST_VELEROIMPORT_YAML
apiVersion: ys.jibudata.com/v1alpha1
kind: VeleroImport
metadata:
  name: $VELEROIMPORT_NAME
  namespace: $TEST_NAMESPACE
spec:
  veleroBackupRef:
    name: $veleroBackupName
    namespace: $veleroBackupNamespace
  restoreNamespace: $restoreNamespace
  pvcNames: $pvcNames
TEMPLATE

    EXEC "kubectl create -f $TEST_VELEROIMPORT_YAML"
}

monitor_cr_status() {
    echo "Monitor CR $VELEROIMPORT_NAME status ..."
    status=""
    while [[ "$status" != "Completed" ]];
    do
        sleep 5
        status=`kubectl get veleroimports.ys.jibudata.com -n $TEST_NAMESPACE $VELEROIMPORT_NAME -o=jsonpath='{.status.phase}'`
        echo "Current VeleroImport status: $status"
    done
    echo "VeleroImport completed."
}

# parse input
while getopts ":b:r:" option; do
    case $option in
        b)
            BACKUP_NAME=${OPTARG}
            echo "backup name: $BACKUP_NAME";;
        r)
            RESTORE_NAMESPACE=${OPTARG}
            echo "restore namespace: $RESTORE_NAMESPACE";;
        *)
            usage
            exit;;
    esac
done
if [[ -z "$BACKUP_NAME" ]] || [[ -z "$RESTORE_NAMESPACE" ]]; then
    usage
    exit 1
fi
prepare
create_cr
monitor_cr_status
