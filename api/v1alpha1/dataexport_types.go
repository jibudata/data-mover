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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ExportPolicy struct {
	Frequency string        `json:"frequency,omitempty"`
	Retention time.Duration `json:"retention,omitempty"`
	// encryption
}

// DataExportSpec defines the desired state of DataExport
type DataExportSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Type         string                  `json:"type"`
	Policy       ExportPolicy            `json:"policy,omitempty"`
	BackupJobRef *corev1.ObjectReference `json:"backupJobRef,omitempty"`
	//DataHandleMap map[string]*DataSource  `json:"dataHandleMap,omitempty"`
	DataHandleMap map[string]string `json:"dataHandleMap,omitempty"` //TBD
}

// DataExportStatus defines the observed state of DataExport
type DataExportStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase               string       `json:"type"`
	Message             string       `json:"message"`
	StartTimestamp      *metav1.Time `json:"startTimestamp,omitempty"`
	CompletionTimestamp *metav1.Time `json:"completionTimestamp,omitempty"`
	//DataLocationMap     map[string]*DataDescriptor `json:"dataLocationMap,omitempty"`
	DataLocationMap map[string]string `json:"dataLocationMap,omitempty"` //TBD
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DataExport is the Schema for the dataexports API
type DataExport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataExportSpec   `json:"spec,omitempty"`
	Status DataExportStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DataExportList contains a list of DataExport
type DataExportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataExport{}, &DataExportList{})
}
