# Kubernetes Cluster API Provider Elf

[![build](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml/badge.svg)](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml)

## 依赖

### K8s 基础集群

一个普通的 k8s 集群，用来作为 k8s 管理集群。

### 管理集群

k8s 基础集群安装 [cluster-api](https://github.com/kubernetes-sigs/cluster-api) 后变成 k8s 管理集群（嵌套集群），管理集群负责 k8s 集群的管理（创建、删除等）。

### 虚拟机模板

创建 k8s 集群时，先在 ELF 创建虚拟机，然后在虚拟机部署 k8s 节点。创建 k8s 集群，需要提供符合 cluster-api 要求的虚拟机。
可以使用 [image-builder](https://github.smartx.com/yiran/image-builder) 构建满足 cluster-api 要求的虚拟机模板，在创建 k8s 集群的时候使用指定的模板。

## 使用方式

**生成创建嵌套集群的配置文件**

提示：请根据​实际情况修改下述配置信息。

```shell
# 嵌套集群的名称
export CLUSTER_NAME=cluster

# 嵌套集群的版本
export KUBERNETES_VERSION=v1.20.6

# 嵌套集群 Master 节点数
export CONTROL_PLANE_MACHINE_COUNT=1

# 嵌套集群 Worker 节点数
export WORKER_MACHINE_COUNT=1

# ELF 集群
export ELF_SERVER=127.0.0.1
export ELF_SERVER_USERNAME=root
export ELF_SERVER_PASSWORD=root

# ELF Master 节点
export CONTROL_PLANE_IP=127.0.0.1
export CONTROL_PLANE_NETMASK=255.255.255.255
export CONTROL_PLANE_GATEWAY=127.0.0.1

# ELF 虚拟机模板
export ELF_TEMPLATE=336820d7-5ba5-4707-9d0c-8f3e583b950f

# 创建嵌套集群的配置文件
clusterctl generate yaml --from templates/cluster-template.yaml > cluster.yaml
```

**创建嵌套集群**

```shell
kubectl apply -f name.yaml
```

**检查嵌套集群部署情况**

```shell
# 获取嵌套集群的 kubeconfig 配置
clusterctl get kubeconfig cluster > cluster.kubeconfig

# 部署 CNI, 可以选择 flannel 或者 calico（可选）
kubectl --kubeconfig=./cluster.kubeconfig \
  apply -f https://raw.githubusercontent.com/coreos/flannel/master/Documentation/kube-flannel.yml

kubectl --kubeconfig=./cluster.kubeconfig \
  apply -f https://docs.projectcalico.org/v3.15/manifests/calico.yaml

# 检查集群的节点
KUBECONFIG=cluster.kubeconfig kubectl get nodes

# 检查集群的组件
KUBECONFIG=cluster.kubeconfig kubectl get pods -n kube-system -o wide
```
