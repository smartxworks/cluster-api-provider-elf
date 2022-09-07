# Testing

本文主要介绍如何进行 CAPE 测试。

## E2E

本小节介绍 CAPE 的端到端测试（E2E）。

### Requirements

运行 E2E 测试的机器需要满足以下条件：
* 可访问的 Tower 服务
* 可以通过 Tower 访问部署在 ELF 集群上的虚拟机
* 安装有 Ginkgo ([download](https://onsi.github.io/ginkgo/#getting-ginkgo))
* 安装有 Docker ([download](https://www.docker.com/get-started))
* 安装有 Kind v0.7.0+ ([download](https://kind.sigs.k8s.io))

### Environment variables

运行 E2E 测试前需要先设置相关的环境变量：

| Environment variable        | Description                          | Example                                                                     |
| --------------------------- | ------------------------------------ | --------------------------------------------------------------------------- |
| `TOWER_SERVER`              | Tower 服务器地址                     | `127.0.0.1`                                                                 |
| `TOWER_USERNAME`            | Tower 用户名                         | `root`                                                                      |
| `TOWER_PASSWORD`            | Tower 用户密码                       | `root`                                                                      |
| `TOWER_AUTH_MODE`           | Tower 认证模式                       | `LOCAL`                                                                     |
| `TOWER_SKIP_TLS_VERIFY`     | Tower 验证服务器CA证书               | `false`                                                                     |
| `ELF_CLUSTER`               | ELF 集群 ID                          | `576ad467-d09e-4235-9dec-b615814ddc7e`                                      |
| `VM_TEMPLATE`              | 用来创建 Kubernetes 节点的虚拟机模板 | `cl7hao0tseso80758osh921f1`                                      |
| `VM_TEMPLATE_UPGRADE_TO`   | 用来升级 Kubernetes 节点的虚拟机模板 | `cl7hfp6lgx7ts0758xf3oza3c`                                      |
| `CONTROL_PLANE_ENDPOINT_IP` | Kubernetes 集群的 IP 地址            | `127.0.0.1`                                                                 |
| `ELF_VLAN`                  | 虚拟网络 ID                          | `576ad467-d09e-4235-9dec-b615814ddc7e_c8a1e42d-e0f3-4d50-a190-53209a98f157` |

### Running the E2E tests

执行以下命令运行 E2E 测试：

```shell
make e2e
```

以上命令会构建 CAPE 本地镜像并使用该镜像进行 E2E 测试。
