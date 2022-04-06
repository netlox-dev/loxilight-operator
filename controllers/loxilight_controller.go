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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	netloxv1alpha1 "github.com/netlox-dev/loxilight-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LoxilightReconciler reconciles a Loxilight object
type LoxilightReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func isOperatorRequest(request ctrl.Request) bool {
	if request.Namespace == "" && request.Name == operatortypes.ClusterConfigName {
		return true
	}
	if request.Namespace == operatortypes.OperatorNameSpace && request.Name == operatortypes.OperatorConfigName {
		return true
	}
	return false
}

//+kubebuilder:rbac:groups=netlox.netlox.io,resources=loxilights,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=netlox.netlox.io,resources=loxilights/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=netlox.netlox.io,resources=loxilights/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Loxilight object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *LoxilightReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if !isOperatorRequest(req) {
		return ctrl.Result{}, nil
	}

	// TODO(user): your logic here
	genericdaemon := o
	ds := newDaemonset(o)
	err := sdk.Create(ds)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	err = sdk.Get(ds)
	if err != nil {
		return err
	}
	if genericdaemon.Status.Count != ds.Status.NumberReady {
		genericdaemon.Status.Count = ds.Status.NumberReady
		err = sdk.Update(genericdaemon)
		if err != nil {
			return err
		}
	}

	return ctrl.Result{}, nil
}

func newDaemonset(cr *v1beta1.GenericDaemon) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-daemonset",
			Namespace: cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cr, schema.GroupVersionKind{
					Group:   v1beta1.SchemeGroupVersion.Group,
					Version: v1beta1.SchemeGroupVersion.Version,
					Kind:    "GenericDaemon",
				}),
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"daemonset": cr.Name + "-daemonset"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"daemonset": cr.Name + "-daemonset"},
				},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"daemon": cr.Spec.Label},
					Containers: []corev1.Container{
						{
							Name:  "genericdaemon",
							Image: cr.Spec.Image,
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *LoxilightReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netloxv1alpha1.Loxilight{}).
		Complete(r)
}
