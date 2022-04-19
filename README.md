# LOXILIGHT Operator

## 1. Create a new project
---
```
$ mkdir loxilight-operator
$ cd loxilight-operator
```

## 2. Create a new API and Controller
---
```
$ operator-sdk init --domain netlox.io --repo github.com/netlox-dev/loxilight-operator
$ operator-sdk create api --group cache --version v1alpha1 --kind Loxilightd --resource --controller
```

## 3. Build image and pusth to image registry
---
```
$ make docker-build docker-push IMG="kongseokhwan/loxilight-operator:v0.0.1"
```

# modify resource
```
$ make generate
```

```
$ make manifests
```

## 4. Run the operator 
---
(1) locally
```
$ make install run
```

(2) Run as a Deployment inside the cluster
```
$ make deploy IMG="kongseokhwan/loxilight-operator:v0.0.1"
```

## 5. Update Custom Resource and run CR
---
```
$ kubectl get deployment -n loxilight-operator-system

$ kubectl apply -f config/samples/cache_v1alpha1_loxilightd.yaml

$ kubectl delete -f config/samples/cache_v1alpha1_loxilightd.yaml
```

## 6. Uninstall operator
---
```
$ make undeploy
```