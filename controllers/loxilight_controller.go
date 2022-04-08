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
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	netloxv1alpha1 "github.com/netlox-dev/loxilight-operator/api/v1alpha1"
	configutil "github.com/netlox-dev/loxilight-operator/controllers/config"
	"github.com/netlox-dev/loxilight-operator/controllers/sharedinfo"
	operatortypes "github.com/netlox-dev/loxilight-operator/controllers/types"
	"github.com/openshift/cluster-network-operator/pkg/apply"
	"github.com/openshift/cluster-network-operator/pkg/controller/statusmanager"
	"github.com/openshift/cluster-network-operator/pkg/render"

	configv1 "github.com/openshift/api/config/v1"
)

var log = ctrl.Log.WithName("controllers")

type Adaptor interface {
	SetupWithManager(r *LoxilightReconciler, mgr ctrl.Manager) error
	Reconcile(r *LoxilightReconciler, request ctrl.Request) (reconcile.Result, error)
	UpdateStatusManagerAndSharedInfo(r *LoxilightReconciler, objs []*uns.Unstructured, clusterConfig *configv1.Network) error
}

type AdaptorK8s struct {
	Config configutil.Config
}

type AdaptorOc struct {
	Config configutil.Config
}

func (k8s *AdaptorK8s) SetupWithManager(r *LoxilightReconciler, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netloxv1alpha1.Loxilight{}).
		Complete(r)
}

func (oc *AdaptorOc) SetupWithManager(r *LoxilightReconciler, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netloxv1alpha1.Loxilight{}).
		Watches(&source.Kind{Type: &configv1.Network{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
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
func (r *LoxilightReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	if !isOperatorRequest(request) {
		return ctrl.Result{}, nil
	}

	// Fetch loxilight CR.
	operConfig, err, found, change := fetchLoxilight(r, request)
	if err != nil && !found {
		log.Error(err, "Failed to get Loxilight CR")
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{Requeue: true}, err
	}
	if !change {
		return reconcile.Result{}, nil
	}

	// Apply configuration.
	if result, err := applyConfig(r, r.Config, nil, operConfig, nil); err != nil {
		return result, err
	}

	// TODO: Start Here
	r.Status.SetNotDegraded(statusmanager.ClusterConfig)
	r.Status.SetNotDegraded(statusmanager.OperatorConfig)

	r.AppliedOperConfig = operConfig

	return ctrl.Result{}, nil
}

func applyConfig(r *LoxilightReconciler, config configutil.Config, clusterConfig *configv1.Network, operConfig *netloxv1alpha1.AntreaInstall, operatorNetwork *ocoperv1.Network) (reconcile.Result, error) {
	// Fill default configurations.
	if err := config.FillConfigs(clusterConfig, operConfig); err != nil {
		log.Error(err, "failed to fill configurations")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "FillConfigurationsError", fmt.Sprintf("Failed to fill configurations: %v", err))
		return reconcile.Result{Requeue: true}, err
	}

	// Validate configurations.
	if err := config.ValidateConfig(clusterConfig, operConfig); err != nil {
		log.Error(err, "failed to validate configurations")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "InvalidOperatorConfig", fmt.Sprintf("The operator configuration is invalid: %v", err))
		return reconcile.Result{Requeue: true}, err
	}

	// Generate render data.
	renderData, err := config.GenerateRenderData(operatorNetwork, operConfig)
	if err != nil {
		log.Error(err, "failed to generate render data")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "RenderConfigError", fmt.Sprintf("Failed to render operator configurations: %v", err))
		return reconcile.Result{Requeue: true}, err
	}

	// Compare configurations change.
	appliedConfig, err := r.getAppliedOperConfig()
	if err != nil {
		log.Error(err, "failed to get applied config")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "InternalError", fmt.Sprintf("Failed to get current configurations: %v", err))
		return reconcile.Result{}, err
	}
	agentNeedChange, controllerNeedChange, imageChange := configutil.NeedApplyChange(appliedConfig, operConfig)
	if !agentNeedChange && !controllerNeedChange {
		log.Info("no configuration change")
	} else {
		// Render configurations.
		objs, err := render.RenderDir(operatortypes.DefaultManifestDir, renderData)
		if err != nil {
			log.Error(err, "failed to render configuration")
			r.Status.SetDegraded(statusmanager.OperatorConfig, "RenderConfigError", fmt.Sprintf("Failed to render operator configurations: %v", err))
			return reconcile.Result{Requeue: true}, err
		}

		// Update status and sharedInfo.
		r.SharedInfo.Lock()
		defer r.SharedInfo.Unlock()
		if err = r.UpdateStatusManagerAndSharedInfo(r, objs, clusterConfig); err != nil {
			return reconcile.Result{Requeue: true}, err
		}

		// Apply configurations.
		for _, obj := range objs {
			if err = apply.ApplyObject(context.TODO(), r.Client, obj); err != nil {
				log.Error(err, "failed to apply resource")
				r.Status.SetDegraded(statusmanager.OperatorConfig, "ApplyObjectsError", fmt.Sprintf("Failed to apply operator configurations: %v", err))
				return reconcile.Result{Requeue: true}, err
			}
		}

		// Delete old antrea-agent and antrea-controller pods.
		if r.AppliedOperConfig != nil && agentNeedChange && !imageChange {
			if err = deleteExistingPods(r.Client, operatortypes.AntreaAgentDaemonSetName); err != nil {
				msg := fmt.Sprintf("DaemonSet %s is not using the latest configuration updates because: %v", operatortypes.AntreaAgentDaemonSetName, err)
				r.Status.SetDegraded(statusmanager.OperatorConfig, "DeleteOldPodsError", msg)
				return reconcile.Result{Requeue: true}, err
			}
		}
		if r.AppliedOperConfig != nil && controllerNeedChange && !imageChange {
			if err = deleteExistingPods(r.Client, operatortypes.AntreaControllerDeploymentName); err != nil {
				msg := fmt.Sprintf("Deployment %s is not using the latest configuration updates because: %v", operatortypes.AntreaControllerDeploymentName, err)
				r.Status.SetDegraded(statusmanager.OperatorConfig, "DeleteOldPodsError", msg)
				return reconcile.Result{Requeue: true}, err
			}
		}
	}
	return reconcile.Result{}, nil
}

func fetchLoxilight(r *LoxilightReconciler, request ctrl.Request) (*netloxv1alpha1.AntreaInstall, error, bool, bool) {
	// Fetch antrea-install CR.
	operConfig := &netloxv1alpha1.AntreaInstall{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: operatortypes.OperatorNameSpace, Name: operatortypes.OperatorConfigName}, operConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("%s CR not found", operatortypes.OperatorConfigName)
			log.Info(msg)
			r.Status.SetDegraded(statusmanager.ClusterConfig, "NoAntreaInstallCR", msg)
			return nil, err, false, false
		}
		log.Error(err, "failed to get antrea-install CR")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "InvalidAntreaInstallCR", fmt.Sprintf("Failed to get operator CR: %v", err))
		return nil, err, true, false
	}
	if request.Name == operConfig.Name && r.AppliedOperConfig != nil {
		if reflect.DeepEqual(operConfig.Spec, r.AppliedOperConfig.Spec) {
			log.Info("no configuration change")
			return operConfig, nil, true, false
		}
	}
	return operConfig, nil, true, true
}

// LoxilightReconciler reconciles a Loxilight object
type LoxilightReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Status *statusmanager.StatusManager
	Mapper meta.RESTMapper

	Adaptor

	SharedInfo           *sharedinfo.SharedInfo
	AppliedClusterConfig *configv1.Network
	AppliedOperConfig    *operatorv1.Loxilight
}

// SetupWithManager sets up the controller with the Manager.
func (r *LoxilightReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netloxv1alpha1.Loxilight{}).
		Complete(r)
}
