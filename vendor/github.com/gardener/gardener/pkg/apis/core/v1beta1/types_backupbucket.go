// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackupBucket holds details about backup bucket
type BackupBucket struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata" protobuf:"bytes,1,opt,name=metadata"`
	// Specification of the Backup Bucket.
	Spec BackupBucketSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`
	// Most recently observed status of the Backup Bucket.
	Status BackupBucketStatus `json:"status" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackupBucketList is a list of BackupBucket objects.
type BackupBucketList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list object metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// Items is the list of BackupBucket.
	Items []BackupBucket `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// BackupBucketSpec is the specification of a Backup Bucket.
type BackupBucketSpec struct {
	// Provider hold the details of cloud provider of the object store.
	Provider BackupBucketProvider `json:"provider" protobuf:"bytes,1,opt,name=provider"`
	// ProviderConfig is the configuration passed to BackupBucket resource.
	// +optional
	ProviderConfig *ProviderConfig `json:"providerConfig,omitempty" protobuf:"bytes,2,opt,name=providerConfig"`
	// SecretRef is a reference to a secret that contains the credentials to access object store.
	SecretRef corev1.SecretReference `json:"secretRef" protobuf:"bytes,3,opt,name=secretRef"`
	// SeedName holds the name of the seed allocated to BackupBucket for running controller.
	// +optional
	SeedName *string `json:"seedName,omitempty" protobuf:"bytes,4,opt,name=seedName"`
}

// BackupBucketStatus holds the most recently observed status of the Backup Bucket.
type BackupBucketStatus struct {
	// ProviderStatus is the configuration passed to BackupBucket resource.
	// +optional
	ProviderStatus *ProviderConfig `json:"providerStatus,omitempty" protobuf:"bytes,1,opt,name=providerStatus"`
	// LastOperation holds information about the last operation on the BackupBucket.
	// +optional
	LastOperation *LastOperation `json:"lastOperation,omitempty" protobuf:"bytes,2,opt,name=lastOperation"`
	// LastError holds information about the last occurred error during an operation.
	// +optional
	LastError *LastError `json:"lastError,omitempty" protobuf:"bytes,3,opt,name=lastError"`
	// ObservedGeneration is the most recent generation observed for this BackupBucket. It corresponds to the
	// BackupBucket's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,4,opt,name=observedGeneration"`
	// GeneratedSecretRef is reference to the secret generated by backup bucket, which
	// will have object store specific credentials.
	// +optional
	GeneratedSecretRef *corev1.SecretReference `json:"generatedSecretRef,omitempty" protobuf:"bytes,5,opt,name=generatedSecretRef"`
}

// BackupBucketProvider holds the details of cloud provider of the object store.
type BackupBucketProvider struct {
	// Type is the type of provider.
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Region is the region of the bucket.
	Region string `json:"region" protobuf:"bytes,2,opt,name=region"`
}
