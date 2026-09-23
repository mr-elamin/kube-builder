/*
Copyright 2026 BSSConnects. All rights reserved.

This file is part of Nawat and is proprietary and confidential.
Unauthorized copying, modification, distribution or use of this file, via any
medium, is strictly prohibited. Use is permitted only under the terms of a
written licence agreement with BSSConnects.

SPDX-License-Identifier: LicenseRef-BSSConnects-Proprietary
*/

package platform

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/mr-elamin/kubebuilder-practice/api/platform/v1alpha1"
)

// ModuleInstanceReconciler reconciles a ModuleInstance object
type ModuleInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.bssconnects.io,resources=moduleinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.bssconnects.io,resources=moduleinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.bssconnects.io,resources=moduleinstances/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModuleInstance object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *ModuleInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ModuleInstance{}).
		Named("platform-moduleinstance").
		Complete(r)
}
