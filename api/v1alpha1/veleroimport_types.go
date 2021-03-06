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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RestoreActions defines the action for a restore
type RestoreActions struct {
	StorageClassMappings map[string]string `json:"storageClassMappings,omitempty"`
}

// VeleroImportSpec defines the desired state of VeleroImport
type VeleroImportSpec struct {
	// Exported velero backupjob name with file system copy
	VeleroBackupRef *corev1.ObjectReference `json:"veleroBackupRef"`

	// Data Import Reference
	DataImportRef *corev1.ObjectReference `json:"dataImportRef"`

	// Namespace that is backuped by velero
	// IncludedNamespaces []string `json:"includedNamespaces"`

	NamespaceMapping map[string]string `json:"namespaceMapping,omitempty"`

	// Names of PVCs need to be imported
	// +optional
	PvcNames []string `json:"pvcNames,omitempty"`

	SnapshotOnly bool `json:"snapshotOnly,omitempty"`

	// Restic rate limit value, in KB/s
	RateLimitValue int `json:"rateLimitValue,omitempty"`

	// Restore actions
	Actions RestoreActions `json:"actions,omitempty"`
}

// VeleroImportStatus defines the observed state of VeleroImport
type VeleroImportStatus struct {
	Phase                string                  `json:"phase"`
	State                string                  `json:"state"`
	Message              string                  `json:"message,omitempty"`
	VeleroRestoreRef     *corev1.ObjectReference `json:"veleroRestoreRef,omitempty"`
	StartTimestamp       *metav1.Time            `json:"startTimestamp,omitempty"`
	CompletionTimestamp  *metav1.Time            `json:"completionTimestamp,omitempty"`
	LastFailureTimestamp *metav1.Time            `json:"lastFailureTimestamp,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VeleroImport is the Schema for the veleroimports API
type VeleroImport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VeleroImportSpec   `json:"spec,omitempty"`
	Status VeleroImportStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VeleroImportList contains a list of VeleroImport
type VeleroImportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VeleroImport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VeleroImport{}, &VeleroImportList{})
}
