# Kubernetes Cluster API Provider ELF

[![build](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml/badge.svg)](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml)

## 准备K8s集群

### CAPI管理集群

首先创建一个普通的k8s集群。在开发环境中，可以使用kind,minikube等创建。参考[CAPI Quick Start Guide](https://cluster-api.sigs.k8s.io/user/quick-start.html).

在k8s基础集群中安装[cluster-api](https://github.com/kubernetes-sigs/cluster-api)后，将其变成CAPI管理集群(Management Cluster)。管理集群负责管理k8s工作负载集群(Workload Cluster)，包括创建、删除、扩容、升级等操作。

### 安装CAPE

安装Cluster API Provider ELF (CAPE):
```
export CONTROLLER_IMG=smartelf/cape-manager-amd64 TAG=dev
make deploy
```

### 准备虚拟机模板

创建 k8s 集群时，先在 ELF 创建虚拟机，然后在虚拟机部署k8s节点。创建k8s集群，需要提供符合cluster-api要求的虚拟机。
可以使用 [image-builder](https://github.smartx.com/yiran/image-builder) 构建满足cluster-api要求的虚拟机模板，在创建k8s工作负载集群的时候使用指定的虚拟机模板。

## 使用方式

**生成工作负载集群的配置文件**

提示：请根据​实际情况修改下述配置信息。

```shell
# CAPE集群对象所在的namespace
export NAMESPACE=default

# 集群的名称
export CLUSTER_NAME=cape-cluster

# 集群的版本
export KUBERNETES_VERSION=v1.20.6

# 集群 Master 节点数
export CONTROL_PLANE_MACHINE_COUNT=1

# 集群 Worker 节点数
export WORKER_MACHINE_COUNT=1

# TOWER
export TOWER_SERVER=<YOUR_TOWER_SERVER_FQDN>
export TOWER_USERNAME=root
export TOWER_PASSWORD=tower

# 以下指定的ELF资源对象的ID对应其API返回的local_id
# ELF集群ID
export ELF_CLUSTER=576ad467-d09e-4235-9dec-b615814ddc7e

# ELF虚拟网络ID
export ELF_VLAN=576ad467-d09e-4235-9dec-b615814ddc7e_c8a1e42d-e0f3-4d50-a190-53209a98f157

# Control plane endpoint
export CONTROL_PLANE_ENDPOINT_IP=<YOUR_CONTROL_PLANE_VIP>

# ELF虚拟机模板
export ELF_TEMPLATE=336820d7-5ba5-4707-9d0c-8f3e583b950f

# 创建工作负载集群的配置文件
clusterctl generate yaml --from templates/cluster-template.yaml > cape-cluster.yaml
```

**创建工作负载集群**

```shell
kubectl apply -f cape-cluster.yaml
```

**检查集群部署情况**

```shell
# 查看部署进度，直到INITIALIZED为true，表示ControlPlane已创建成功
kubectl get cluster
kubectl get kubeadmcontrolplane -w

# 获取集群的 kubeconfig 配置
clusterctl get kubeconfig cape-cluster > cape-cluster.kubeconfig

# 部署 CNI, 可以选择 flannel 或者 calico（可选）
kubectl --kubeconfig=./cape-cluster.kubeconfig \
  apply -f https://raw.githubusercontent.com/coreos/flannel/master/Documentation/kube-flannel.yml

kubectl --kubeconfig=./cape-cluster.kubeconfig \
  apply -f https://docs.projectcalico.org/v3.15/manifests/calico.yaml

# 检查集群的节点
KUBECONFIG=cape-cluster.kubeconfig kubectl get nodes

# 检查集群的组件
KUBECONFIG=cape-cluster.kubeconfig kubectl get pods -A -o wide
```
