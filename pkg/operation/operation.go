package operation

import (
	"github.com/go-logr/logr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type Operation struct {
	logger logr.Logger
	client k8sclient.Client
	// dmNamespace string
}

// func NewOperation(logger logr.Logger, client k8sclient.Client, dmNamespace string) *Operation {
func NewOperation(logger logr.Logger, client k8sclient.Client) *Operation {
	return &Operation{
		logger: logger,
		client: client,
		// dmNamespace: dmNamespace,
	}

}
