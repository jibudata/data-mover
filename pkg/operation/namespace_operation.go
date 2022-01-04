package operation

import (
	"context"
	"fmt"
	"time"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *Operation) CreateNamespace(ns string, forceRecreate bool) error {
	namespace := core.Namespace{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: ns,
		Name:      ns,
	}, &namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			//create namespace and return
			namespace = core.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}
			err = o.client.Create(context.TODO(), &namespace)
			if err != nil {
				o.logger.Error(err, fmt.Sprintf("Failed to delete namespace %s", ns))
				return err
			}
			return nil

		} else {
			o.logger.Error(err, fmt.Sprintf("Failed to get namespace %s", ns))
			return err
		}
	}
	if !forceRecreate {
		// If namespace exist and do not force recreate namespace, skip recreate
		return nil
	}

	err = o.SyncDeleteNamespace(ns)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to delete original namespace %s", ns))
		return err
	}
	namespace = core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}
	err = o.client.Create(context.TODO(), &namespace)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to create namespace %s", ns))
		return err
	}
	return nil
}

func (o *Operation) AsyncDeleteNamespace(ns string) error {
	namespace := &core.Namespace{}
	err := o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Name: ns,
	}, namespace)
	if err != nil {
		// o.logger.Error(err, fmt.Sprintf("Failed to get namespace %s", ns))
		// If namespace not exist, skip
		if serr, ok := err.(*errors.StatusError); ok {
			if serr.Status().Reason == "NotFound" {
				o.logger.Info(fmt.Sprintf("Skip deleting non-existing namespace %s", ns))
				return nil
			}
		}
		return err
	}
	err = o.client.Delete(context.TODO(), namespace)
	return err
}

func (o *Operation) SyncDeleteNamespace(namespace string) error {
	err := o.AsyncDeleteNamespace(namespace)
	if err != nil {
		return err
	}
	err = o.MonitorDeleteNamespace(namespace)
	return err
}

func (o *Operation) MonitorDeleteNamespace(namespace string) error {
	var err error
	err = nil
	for err == nil {
		time.Sleep(time.Duration(15) * time.Second)
		_, err = o.GetNamespace(namespace)
	}
	return nil
}

func (o *Operation) GetNamespace(namespace string) (*core.Namespace, error) {
	var err error
	err = nil

	ns := &core.Namespace{}
	err = o.client.Get(context.TODO(), k8sclient.ObjectKey{
		Namespace: namespace,
		Name:      namespace,
	}, ns)
	return ns, err
}
