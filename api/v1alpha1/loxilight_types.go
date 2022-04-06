/*
Copyright 2022.

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
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LoxilightSpec defines the desired state of Loxilight
type LoxilightSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// LoxilightAgentConfig holds the configurations for Loxilight-agent.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +required
	LoxilightAgentConfig string `json:"loxilightAgentConfig"`

	// LoxilightCNIConfig holds the configuration of CNI.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +required
	LoxilightCNIConfig string `json:"loxilightCNIConfig"`

	// LoxilightControllerConfig holds the configurations for Loxilight-controller.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +required
	LoxilightControllerConfig string `json:"loxilightControllerConfig"`

	// LoxilightPlatform is the platform on which Loxilight will be deployed.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +required
	LoxilightPlatform string `json:"loxilightPlatform"`

	// LoxilightImage is the Docker image name used by Loxilight-agent and Loxilight-controller.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	LoxilightImage string `json:"loxilightImage,omitempty"`
}

// +kubebuilder:object:generate=false
type InstallCondition = configv1.ClusterOperatorStatusCondition

// LoxilightStatus defines the observed state of Loxilight
type LoxilightStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions describes the state of Antrea installation.
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	Conditions []InstallCondition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Loxilight is the Schema for the loxilights API
type Loxilight struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoxilightSpec   `json:"spec,omitempty"`
	Status LoxilightStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LoxilightList contains a list of Loxilight
type LoxilightList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Loxilight `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Loxilight{}, &LoxilightList{})
}
