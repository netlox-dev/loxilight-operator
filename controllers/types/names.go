package types

const (
	LoxilightClusterOperatorName = "loxilight"
	LoxilightImageRenderKey      = "LoxilightImage"
	ReleaseVersion               = "ReleaseVersion"

	LoxilightAgentConfigOption    = "loxilight-agent.conf"
	LoxilightAgentConfigRenderKey = "LoxilightAgentConfig"

	LoxilightCNIConfigOption    = "loxilight-cni.conflist"
	LoxilightCNIConfigRenderKey = "LoxilightCNIConfig"

	LoxilightControllerConfigOption    = "loxilight-controller.conf"
	LoxilightControllerConfigRenderKey = "LoxilightControllerConfig"

	ServiceCIDROption = "serviceCIDR"
	DefaultMTUOption  = "defaultMTU"

	OperatorNameSpace          = "loxilight-operator"
	ClusterConfigName          = "cluster"
	OperatorConfigName         = "loxilight-install"
	ClusterOperatorNetworkName = "cluster"

	LoxilightNamespace                = "kube-system"
	LoxilightAgentDaemonSetName       = "loxilight-agent"
	LoxilightControllerDeploymentName = "loxilight-controller"
	LoxilightConfigMapName            = "loxilight-config"

	CNIConfDirRenderKey = "CNIConfDir"
	CNIBinDirRenderKey  = "CNIBinDir"
)
