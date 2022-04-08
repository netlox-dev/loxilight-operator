package sharedinfo

import (
	"context"
	"errors"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	netloxv1alpha1 "github.com/netlox-dev/loxilight-operator/api/v1alpha1"
	operatortypes "github.com/netlox-dev/loxilight-operator/controllers/types"
)

var log = logf.Log.WithName("shared_info")

type SharedInfo struct {
	sync.Mutex

	LoxilightPlatform                 string
	LoxilightAgentDaemonSetSpec       *unstructured.Unstructured
	LoxilightControllerDeploymentSpec *unstructured.Unstructured
}

func New(mgr manager.Manager) (*SharedInfo, error) {
	reader := mgr.GetAPIReader()
	loxilightName := types.NamespacedName{
		Name:      operatortypes.OperatorConfigName,
		Namespace: operatortypes.OperatorNameSpace,
	}
	loxilight := &netloxv1alpha1.Loxilight{}
	err := reader.Get(context.TODO(), loxilightName, loxilight)
	if err != nil {
		log.Error(err, "failed to get loxilight", "namespace", operatortypes.OperatorNameSpace, "name", operatortypes.OperatorConfigName)
		return nil, err
	}
	switch loxilight.Spec.LoxilightPlatform {
	case "openshift", "kubernetes":
		return &SharedInfo{LoxilightPlatform: loxilight.Spec.LoxilightPlatform}, nil
	default:
		return nil, errors.New("invalid platform: platform should be openshift or kubernetes")
	}
}
