package operation

import (
	"context"
	"fmt"
	"strings"

	dmapi "github.com/jibudata/data-mover/api/v1alpha1"
	"github.com/jibudata/data-mover/pkg/util"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var CompletedStates = []string{dmapi.StateCanceled, dmapi.StateCompleted, dmapi.StateVeleroFailed}

func (o *Operation) CheckOngoingExport(export *dmapi.VeleroExport) (bool, string, error) {
	veleroexports, err := o.ListVeleroExport(export.Namespace)
	if err != nil {
		return false, "", err
	}
	ns := getNamespaces(export.Spec.DataSourceMapping)
	for _, exportJob := range veleroexports {
		if exportJob.Status.Phase != dmapi.PhaseCreated && (exportJob.Status.State == dmapi.StateInProgress || exportJob.Status.State == dmapi.StateFailed) {
			// if exportJob.Status.State != "" && !util.Contains(CompletedStates, exportJob.Status.State) {
			namespaceMap := getNamespaces(exportJob.Spec.DataSourceMapping)
			for key := range namespaceMap {
				if ns[key] {
					o.logger.Info("find ongoing velero export in cluster dealing with same resource", "ongoing job", exportJob.Name)
					return true, exportJob.Name, nil
				}
			}
		}
	}
	return false, "", nil

}

func getNamespaces(resourceMapping map[string]string) map[string]bool {
	namespaces := make(map[string]bool)
	for key := range resourceMapping {
		namespace := key[:strings.Index(key, "/")]
		namespaces[namespace] = true
	}
	return namespaces
}

func (o *Operation) ListVeleroExport(namespace string) ([]dmapi.VeleroExport, error) {
	exportList := &dmapi.VeleroExportList{}
	err := o.client.List(
		context.TODO(),
		exportList,
		&k8sclient.ListOptions{
			Namespace: namespace,
		},
	)
	if err != nil {
		msg := fmt.Sprintf("failed to list velero export in namespace %s", namespace)
		o.logger.Error(err, msg)
		return nil, util.WrapError(msg, err)
	}
	return exportList.Items, err
}
