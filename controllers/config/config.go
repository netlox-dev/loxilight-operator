package config

import (
	"errors"
	"fmt"

	gocni "github.com/containerd/go-cni"
	configv1 "github.com/openshift/api/config/v1"
	ocoperv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-network-operator/pkg/network"
	"github.com/openshift/cluster-network-operator/pkg/render"
	"gopkg.in/yaml.v2"
	ctrl "sigs.k8s.io/controller-runtime"

	operatorv1 "github.com/netlox-dev/loxilight-operator/api/v1alpha1"
	"github.com/netlox-dev/loxilight-operator/controllers/types"
)

var log = ctrl.Log.WithName("config")

type Config interface {
	FillConfigs(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error
	ValidateConfig(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error
	GenerateRenderData(operatorNetwork *ocoperv1.Network, operConfig *operatorv1.Loxilight) (*render.RenderData, error)
}

type ConfigOc struct{}

type ConfigK8s struct{}

func fillConfig(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	loxilightAgentConfig := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(operConfig.Spec.LoxilightAgentConfig), &loxilightAgentConfig)
	if err != nil {
		return fmt.Errorf("failed to parse LoxilightAgentConfig: %v", err)
	}
	// Set service CIDR.
	if clusterConfig == nil {
		if _, ok := loxilightAgentConfig[types.ServiceCIDROption]; !ok {
			return errors.New("serviceCIDR should be specified on kubernetes.")
		}
	} else {
		if serviceCIDR, ok := loxilightAgentConfig[types.ServiceCIDROption].(string); !ok {
			loxilightAgentConfig[types.ServiceCIDROption] = clusterConfig.Spec.ServiceNetwork[0]
		} else if found := inSlice(serviceCIDR, clusterConfig.Spec.ServiceNetwork); !found {
			log.Info("WARNING: option: %s is overwritten by cluster config")
			loxilightAgentConfig[types.ServiceCIDROption] = clusterConfig.Spec.ServiceNetwork[0]
		}
	}
	// Set default MTU.
	_, ok := loxilightAgentConfig[types.DefaultMTUOption]
	if !ok {
		loxilightAgentConfig[types.DefaultMTUOption] = types.DefaultMTU
	}
	// Set Loxilight image.
	if operConfig.Spec.LoxilightImage == "" {
		operConfig.Spec.LoxilightImage = types.DefaultLoxilightImage
	}
	updatedLoxilightAgentConfig, err := yaml.Marshal(loxilightAgentConfig)
	if err != nil {
		return fmt.Errorf("failed to fill configurations in LoxilightAgentConfig: %v", err)
	}
	operConfig.Spec.LoxilightAgentConfig = string(updatedLoxilightAgentConfig)
	return nil
}

func (c *ConfigOc) FillConfigs(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	return fillConfig(clusterConfig, operConfig)
}

func (c *ConfigK8s) FillConfigs(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	return fillConfig(clusterConfig, operConfig)
}

func validateConfig(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	var errs []error

	if operConfig.Spec.LoxilightImage == "" {
		errs = append(errs, fmt.Errorf("LoxilightImage option can not be empty"))
	}

	loxilightAgentConfig := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(operConfig.Spec.LoxilightAgentConfig), &loxilightAgentConfig)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to parse LoxilightAgentConfig: %v", err))
		return fmt.Errorf("invalidate configuration: %v", errs)
	}

	if clusterConfig == nil {
		if _, ok := loxilightAgentConfig[types.ServiceCIDROption]; !ok {
			errs = append(errs, fmt.Errorf("serviceCIDR option can not be empty"))
		}
	} else {
		if serviceCIDR, ok := loxilightAgentConfig[types.ServiceCIDROption].(string); !ok {
			errs = append(errs, fmt.Errorf("serviceCIDR option can not be empty"))
		} else if found := inSlice(serviceCIDR, clusterConfig.Spec.ServiceNetwork); !found {
			errs = append(errs, fmt.Errorf("invalid serviceCIDR option: %s, available values are: %s", serviceCIDR, clusterConfig.Spec.ServiceNetwork))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalidate configuration: %v", errs)
	}
	return nil
}

func (c *ConfigOc) ValidateConfig(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	return validateConfig(clusterConfig, operConfig)
}

func (c *ConfigK8s) ValidateConfig(clusterConfig *configv1.Network, operConfig *operatorv1.Loxilight) error {
	return validateConfig(clusterConfig, operConfig)
}

func NeedApplyChange(preConfig, curConfig *operatorv1.Loxilight) (agentNeedChange, controllerNeedChange, imageChange bool) {
	if preConfig == nil {
		return true, true, false
	}

	if preConfig.Spec.LoxilightAgentConfig != curConfig.Spec.LoxilightAgentConfig {
		agentNeedChange = true
	}
	if preConfig.Spec.LoxilightCNIConfig != curConfig.Spec.LoxilightCNIConfig {
		agentNeedChange = true
	}
	if preConfig.Spec.LoxilightControllerConfig != curConfig.Spec.LoxilightControllerConfig {
		controllerNeedChange = true
	}
	if preConfig.Spec.LoxilightImage != curConfig.Spec.LoxilightImage {
		agentNeedChange = true
		controllerNeedChange = true
		imageChange = true
	}
	return
}

func HasClusterNetworkConfigChange(preConfig, curConfig *configv1.Network) bool {
	// TODO: We may need to save the applied cluster network config in somewhere else. Thus operator can
	// retrieve the applied config on restart.
	if preConfig == nil {
		return true
	}
	if !stringSliceEqual(preConfig.Spec.ServiceNetwork, curConfig.Spec.ServiceNetwork) {
		return true
	}
	var preCIDRs, curCIDRs []string
	for _, clusterNet := range preConfig.Spec.ClusterNetwork {
		preCIDRs = append(preCIDRs, clusterNet.CIDR)
	}
	for _, clusterNet := range curConfig.Spec.ClusterNetwork {
		curCIDRs = append(curCIDRs, clusterNet.CIDR)
	}
	if !stringSliceEqual(preCIDRs, curCIDRs) {
		return true
	}
	return false
}

func HasDefaultMTUChange(preConfig, curConfig *operatorv1.Loxilight) (bool, int, error) {

	curLoxilightAgentConfig := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(curConfig.Spec.LoxilightAgentConfig), &curLoxilightAgentConfig)
	if err != nil {
		return false, types.DefaultMTU, err
	}
	curDefaultMTU, ok := curLoxilightAgentConfig[types.DefaultMTUOption]
	if !ok {
		return false, types.DefaultMTU, fmt.Errorf("%s option can not be empty", types.DefaultMTUOption)
	}

	if preConfig == nil {
		return true, curDefaultMTU.(int), nil
	}

	preLoxilightAgentConfig := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(preConfig.Spec.LoxilightAgentConfig), &preLoxilightAgentConfig)
	if err != nil {
		return false, types.DefaultMTU, err
	}
	preDefaultMTU, ok := preLoxilightAgentConfig[types.DefaultMTUOption]
	if !ok {
		return false, types.DefaultMTU, fmt.Errorf("%s option can not be empty", types.DefaultMTUOption)
	}

	return preDefaultMTU != curDefaultMTU, curDefaultMTU.(int), nil
}

func BuildNetworkStatus(clusterConfig *configv1.Network, defaultMTU int) *configv1.NetworkStatus {
	// Values extracted from spec are serviceNetwork and clusterNetworkCIDR.
	status := configv1.NetworkStatus{}
	for _, snet := range clusterConfig.Spec.ServiceNetwork {
		status.ServiceNetwork = append(status.ServiceNetwork, snet)
	}

	for _, cnet := range clusterConfig.Spec.ClusterNetwork {
		status.ClusterNetwork = append(status.ClusterNetwork,
			configv1.ClusterNetworkEntry{
				CIDR:       cnet.CIDR,
				HostPrefix: cnet.HostPrefix,
			})
	}
	status.NetworkType = clusterConfig.Spec.NetworkType
	status.ClusterNetworkMTU = defaultMTU
	return &status
}

func inSlice(str string, s []string) bool {
	for _, v := range s {
		if str == v {
			return true
		}
	}
	return false
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !inSlice(v, b) {
			return false
		}
	}
	return true
}

// pluginCNIDir is the directory where plugins should install their CNI
// configuration file. By default, it is where multus looks, unless multus
// is deactivated
func pluginCNIConfDir(conf *ocoperv1.NetworkSpec) string {
	if conf.DisableMultiNetwork == nil || !*conf.DisableMultiNetwork {
		return network.MultusCNIConfDir
	}
	return network.SystemCNIConfDir
}

func generateRenderData(operatorNetwork *ocoperv1.Network, operConfig *operatorv1.Loxilight) *render.RenderData {
	renderData := render.MakeRenderData()
	renderData.Data[types.ReleaseVersion] = "1.0.0"
	renderData.Data[types.LoxilightAgentConfigRenderKey] = operConfig.Spec.LoxilightAgentConfig
	renderData.Data[types.LoxilightCNIConfigRenderKey] = operConfig.Spec.LoxilightCNIConfig
	renderData.Data[types.LoxilightControllerConfigRenderKey] = operConfig.Spec.LoxilightControllerConfig
	renderData.Data[types.LoxilightImageRenderKey] = operConfig.Spec.LoxilightImage
	if operatorNetwork == nil {
		renderData.Data[types.CNIConfDirRenderKey] = gocni.DefaultNetDir
		renderData.Data[types.CNIBinDirRenderKey] = gocni.DefaultCNIDir
	} else {
		renderData.Data[types.CNIConfDirRenderKey] = pluginCNIConfDir(&operatorNetwork.Spec)
		renderData.Data[types.CNIBinDirRenderKey] = network.CNIBinDir
	}
	return &renderData
}

func (c *ConfigK8s) GenerateRenderData(operatorNetwork *ocoperv1.Network, operConfig *operatorv1.Loxilight) (*render.RenderData, error) {
	renderData := generateRenderData(operatorNetwork, operConfig)
	return renderData, nil
}

func (c *ConfigOc) GenerateRenderData(operatorNetwork *ocoperv1.Network, operConfig *operatorv1.Loxilight) (*render.RenderData, error) {
	renderData := generateRenderData(operatorNetwork, operConfig)
	return renderData, nil
}
