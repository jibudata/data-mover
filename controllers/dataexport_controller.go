/*
Copyright 2021.

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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	ysv1alpha1 "github.com/jibudata/data-mover/api/v1alpha1"
)

// DataExportReconciler reconciles a DataExport object
type DataExportReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ys.jibudata.com,resources=dataexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=dataexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ys.jibudata.com,resources=dataexports/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DataExport object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *DataExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var err error

	// Retrieve the DataExport object to be retrieved
	dataExport := &ysv1alpha1.DataExport{}
	err = r.Get(ctx, req.NamespacedName, dataExport)
	if err != nil {
		r.Log.Error(err, "")
		return ctrl.Result{Requeue: true}, nil
	}

	// Report reconcile error.
	defer func() {
		if err == nil || errors.IsConflict(err) {
			return
		}
		dataExport.Status.SetReconcileFailed(err)
		err := r.Update(ctx, dataExport)
		if err != nil {
			//r.Log.Error(err, "")
			return
		}
	}()

	// TBD: Main logic

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataExportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ysv1alpha1.DataExport{}).
		Complete(r)
}
