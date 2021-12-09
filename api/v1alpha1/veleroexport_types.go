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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VeleroExportSpec defines the desired state of VeleroExport
type VeleroExportSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Velero backupjob name with volume snapshots
	VeleroBackupRef *corev1.ObjectReference `json:"veleroBackupRef"`

	// Namespace that is backuped by velero
	BackupNamespace string `json:"backupNamespace"`

	// DataSourceMapping is a map of pvc names to volumesnapshot names to be exported.
	// +optional
	DataSourceMapping map[string]string `json:"dataSourceMapping,omitempty"`
}

// VeleroExportStatus defines the observed state of VeleroExport
type VeleroExportStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase               string                  `json:"phase"`
	State               string                  `json:"state"`
	Message             string                  `json:"message"`
	VeleroBackupRef     *corev1.ObjectReference `json:"veleroBackupName,omitempty"`
	StartTimestamp      *metav1.Time            `json:"startTimestamp,omitempty"`
	CompletionTimestamp *metav1.Time            `json:"completionTimestamp,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VeleroExport is the Schema for the veleroexports API
type VeleroExport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VeleroExportSpec   `json:"spec,omitempty"`
	Status VeleroExportStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VeleroExportList contains a list of VeleroExport
type VeleroExportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VeleroExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VeleroExport{}, &VeleroExportList{})
}
