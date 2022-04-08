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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	uns "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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

func isOperatorRequest(r *LoxilightReconciler, request ctrl.Request) bool {
	reqLogger := r.Log.WithValues("Request.NamespacedName", request.NamespacedName)
	if request.Namespace == "" && request.Name == operatortypes.ClusterConfigName {
		reqLogger.Info("Reconciling loxilight-operator Cluster Network CR change")
		return true
	}
	if request.Namespace == operatortypes.OperatorNameSpace && request.Name == operatortypes.OperatorConfigName {
		reqLogger.Info("Reconciling loxilight-operator loxilight-install CR change")
		return true
	}
	return false
}

func (k8s *AdaptorK8s) Reconcile(r *LoxilightReconciler, request ctrl.Request) (reconcile.Result, error) {
	if !isOperatorRequest(r, request) {
		return reconcile.Result{}, nil
	}

	// Fetch antrea-install CR.
	operConfig, err, found, change := fetchLoxilight(r, request)
	if err != nil && !found {
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{Requeue: true}, err
	}
	if !change {
		return reconcile.Result{}, nil
	}

	// Apply configuration.
	if result, err := applyConfig(r, k8s.Config, nil, operConfig, nil); err != nil {
		return result, err
	}

	r.Status.SetNotDegraded(statusmanager.ClusterConfig)
	r.Status.SetNotDegraded(statusmanager.OperatorConfig)

	r.AppliedOperConfig = operConfig

	return reconcile.Result{}, nil
}

func (oc *AdaptorOc) Reconcile(r *LoxilightReconciler, request ctrl.Request) (reconcile.Result, error) {
	if !isOperatorRequest(r, request) {
		return reconcile.Result{}, nil
	}

	// Fetch Cluster Network CR.
	clusterConfig := &configv1.Network{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: operatortypes.ClusterConfigName}, clusterConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			msg := "Cluster Network CR not found"
			log.Info(msg)
			r.Status.SetDegraded(statusmanager.ClusterConfig, "NoClusterConfig", msg)
			return reconcile.Result{}, nil
		}
		r.Status.SetDegraded(statusmanager.ClusterConfig, "InvalidClusterConfig", fmt.Sprintf("Failed to get cluster network CRD: %v", err))
		log.Error(err, "failed to get Cluster Network CR")
		return reconcile.Result{Requeue: true}, err
	}
	if request.Name == clusterConfig.Name && r.AppliedClusterConfig != nil {
		if reflect.DeepEqual(clusterConfig.Spec, r.AppliedClusterConfig.Spec) {
			log.Info("no configuration change")
			return reconcile.Result{}, nil
		}
	}

	// Fetch the Network.operator.openshift.io instance
	operatorNetwork := &ocoperv1.Network{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: operatortypes.ClusterOperatorNetworkName}, operatorNetwork)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.Status.SetDegraded(statusmanager.OperatorConfig, "NoClusterNetworkOperatorConfig", fmt.Sprintf("Cluster network operator configuration not found"))
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Unable to retrieve Network.operator.openshift.io object")
		return reconcile.Result{Requeue: true}, err
	}

	// Fetch antrea-install CR.
	operConfig, err, found, change := fetchLoxilight(r, request)
	if err != nil && !found {
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{Requeue: true}, err
	}
	if !change {
		return reconcile.Result{}, nil
	}

	// Apply configuration.
	if result, err := applyConfig(r, oc.Config, clusterConfig, operConfig, operatorNetwork); err != nil {
		return result, err
	}

	// Update cluster network CR status.
	clusterNetworkConfigChanged := configutil.HasClusterNetworkConfigChange(r.AppliedClusterConfig, clusterConfig)
	defaultMTUChanged, curDefaultMTU, err := configutil.HasDefaultMTUChange(r.AppliedOperConfig, operConfig)
	if err != nil {
		r.Status.SetDegraded(statusmanager.OperatorConfig, "UpdateNetworkStatusError", fmt.Sprintf("failed to check default MTU configuration: %v", err))
		return reconcile.Result{Requeue: true}, err
	}
	if clusterNetworkConfigChanged || defaultMTUChanged {
		if err = updateNetworkStatus(r.Client, clusterConfig, curDefaultMTU); err != nil {
			r.Status.SetDegraded(statusmanager.ClusterConfig, "UpdateNetworkStatusError", fmt.Sprintf("Failed to update network status: %v", err))
			return reconcile.Result{Requeue: true}, err
		}
	}

	r.Status.SetNotDegraded(statusmanager.ClusterConfig)
	r.Status.SetNotDegraded(statusmanager.OperatorConfig)

	r.AppliedClusterConfig = clusterConfig
	r.AppliedOperConfig = operConfig

	return reconcile.Result{}, nil
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
	return r.Adaptor.Reconcile(r, request)
}

func (r *LoxilightReconciler) getAppliedOperConfig() (*netloxv1alpha1.Loxilight, error) {
	if r.AppliedOperConfig != nil {
		return r.AppliedOperConfig, nil
	}
	operConfig := &netloxv1alpha1.Loxilight{}
	var loxilightConfig *corev1.ConfigMap
	configList := &corev1.ConfigMapList{}
	label := map[string]string{"app": "antrea"}
	if err := r.Client.List(context.TODO(), configList, client.InNamespace(operatortypes.LoxilightNamespace), client.MatchingLabels(label)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	for i := range configList.Items {
		if strings.HasPrefix(configList.Items[i].Name, operatortypes.LoxilightConfigMapName) {
			loxilightConfig = &configList.Items[i]
			break
		}
	}
	if loxilightConfig == nil {
		log.Info("no antrea-config found")
		return nil, nil
	}
	loxilightControllerDeployment := appsv1.Deployment{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: operatortypes.LoxilightNamespace, Name: operatortypes.LoxilightControllerDeploymentName}, &loxilightControllerDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	image := loxilightControllerDeployment.Spec.Template.Spec.Containers[0].Image
	operConfigSpec := netloxv1alpha1.LoxilightSpec{
		LoxilightAgentConfig:      loxilightConfig.Data[operatortypes.LoxilightAgentConfigOption],
		LoxilightCNIConfig:        loxilightConfig.Data[operatortypes.LoxilightCNIConfigOption],
		LoxilightControllerConfig: loxilightConfig.Data[operatortypes.LoxilightControllerConfigOption],
		LoxilightImage:            image,
	}
	operConfig.Spec = operConfigSpec
	return operConfig, nil
}

func applyConfig(r *LoxilightReconciler, config configutil.Config, clusterConfig *configv1.Network, operConfig *netloxv1alpha1.Loxilight, operatorNetwork *ocoperv1.Network) (reconcile.Result, error) {
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

func fetchLoxilight(r *LoxilightReconciler, request ctrl.Request) (*netloxv1alpha1.Loxilight, error, bool, bool) {
	// Fetch loxilight-install CR.
	operConfig := &netloxv1alpha1.Loxilight{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: operatortypes.OperatorNameSpace, Name: operatortypes.OperatorConfigName}, operConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("%s CR not found", operatortypes.OperatorConfigName)
			log.Info(msg)
			r.Status.SetDegraded(statusmanager.ClusterConfig, "NoLoxilightCR", msg)
			return nil, err, false, false
		}
		log.Error(err, "failed to get loxilight-install CR")
		r.Status.SetDegraded(statusmanager.OperatorConfig, "InvalidloxilightInstallCR", fmt.Sprintf("Failed to get operator CR: %v", err))
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
	AppliedOperConfig    *netloxv1alpha1.Loxilight
}

// SetupWithManager sets up the controller with the Manager.
func (r *LoxilightReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netloxv1alpha1.Loxilight{}).
		Complete(r)
}

func updateStatusManagerAndSharedInfo(r *LoxilightReconciler, objs []*uns.Unstructured, clusterConfig *configv1.Network) error {
	var daemonSets, deployments []types.NamespacedName
	var relatedObjects []configv1.ObjectReference
	var daemonSetObject, deploymentObject *uns.Unstructured
	for _, obj := range objs {
		if obj.GetAPIVersion() == "apps/v1" && obj.GetKind() == "DaemonSet" {
			daemonSets = append(daemonSets, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
			daemonSetObject = obj
		} else if obj.GetAPIVersion() == "apps/v1" && obj.GetKind() == "Deployment" {
			deployments = append(deployments, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
			deploymentObject = obj
		}
		restMapping, err := r.Mapper.RESTMapping(obj.GroupVersionKind().GroupKind())
		if err != nil {
			log.Error(err, "failed to get REST mapping for storing related object")
			continue
		}
		relatedObjects = append(relatedObjects, configv1.ObjectReference{
			Group:     obj.GetObjectKind().GroupVersionKind().Group,
			Resource:  restMapping.Resource.Resource,
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		})
		if clusterConfig != nil {
			if err := controllerutil.SetControllerReference(clusterConfig, obj, r.Scheme); err != nil {
				log.Error(err, "failed to set owner reference", "resource", obj.GetName())
				r.Status.SetDegraded(statusmanager.OperatorConfig, "ApplyObjectsError", fmt.Sprintf("Failed to set owner reference: %v", err))
				return err
			}
		}
	}
	if daemonSetObject == nil || deploymentObject == nil {
		var missedResources []string
		if daemonSetObject == nil {
			missedResources = append(missedResources, fmt.Sprintf("DaemonSet: %s", operatortypes.AntreaAgentDaemonSetName))
		}
		if deploymentObject == nil {
			missedResources = append(missedResources, fmt.Sprintf("Deployment: %s", operatortypes.AntreaControllerDeploymentName))
		}
		err := fmt.Errorf("configuration of resources %v is missing", missedResources)
		log.Error(nil, err.Error())
		r.Status.SetDegraded(statusmanager.OperatorConfig, "ApplyObjectsError", err.Error())
		return err
	}
	r.Status.SetDaemonSets(daemonSets)
	r.Status.SetDeployments(deployments)
	r.Status.SetRelatedObjects(relatedObjects)
	r.SharedInfo.LoxilightAgentDaemonSetSpec = daemonSetObject.DeepCopy()
	r.SharedInfo.LoxilightControllerDeploymentSpec = deploymentObject.DeepCopy()
	return nil
}

func (a *AdaptorK8s) UpdateStatusManagerAndSharedInfo(r *LoxilightReconciler, objs []*uns.Unstructured, clusterConfig *configv1.Network) error {
	return updateStatusManagerAndSharedInfo(r, objs, clusterConfig)
}

func (a *AdaptorOc) UpdateStatusManagerAndSharedInfo(r *LoxilightReconciler, objs []*uns.Unstructured, clusterConfig *configv1.Network) error {
	return updateStatusManagerAndSharedInfo(r, objs, clusterConfig)
}
