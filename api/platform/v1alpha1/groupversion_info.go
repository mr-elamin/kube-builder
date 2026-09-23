/*
Copyright 2026 BSSConnects. All rights reserved.

This file is part of Nawat and is proprietary and confidential.
Unauthorized copying, modification, distribution or use of this file, via any
medium, is strictly prohibited. Use is permitted only under the terms of a
written licence agreement with BSSConnects.

SPDX-License-Identifier: LicenseRef-BSSConnects-Proprietary
*/

// Package v1alpha1 contains API Schema definitions for the platform v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=platform.bssconnects.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// SchemeGroupVersion is group version used to register these objects.
	// This name is used by applyconfiguration generators (e.g. controller-gen).
	SchemeGroupVersion = schema.GroupVersion{Group: "platform.bssconnects.io", Version: "v1alpha1"}

	// GroupVersion is an alias for SchemeGroupVersion, for backward compatibility.
	GroupVersion = SchemeGroupVersion

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
		return nil
	})

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
