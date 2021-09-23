# 发布流程

1. 如果发布的是新的 minor 版本，创建一个新的发布分支并推送到 GitHub，否则切换到该分支，例如 `release-0.6`
2. 设置版本环境变量 `VERSION=v0.x.x`，版本以 v 为前缀，例如 v0.0.6
3. 打标签 `git tag -m $VERSION $VERSION`
4. 推送标签到 GitHub `git push upstream $VERSION`
5. 标签推送到 GitHub 后会自动触发 [Github Action](https://github.com/smartxworks/cluster-api-provider-elf) 创建一个[待发布版本](https://github.com/smartxworks/cluster-api-provider-elf/releases)
+ 5.1 构建 docker 镜像
+ 5.2 推送 docker 镜像到镜像仓库
+ 5.3 生成 manifests
+ 5.4 创建待发布版本
+ 5.5 上传 manifests 到待发布版本的 Assets
6. 检查 GitHub 的待发布版本和镜像
7. 发布新版本
