# logsnap：一个最小的 containerd 代理快照器

logsnap 把 containerd 自带的 overlayfs 快照器包装成一个独立进程，通过 unix socket 提供 containerd 的 Snapshots gRPC API，并把每次调用的方法、namespace、参数和耗时以 JSON 日志打印出来。

它适合用来：
- 学习 proxy plugin 的写法（核心代码不到 100 行）；
- 观察 containerd 在拉镜像、启动容器时实际调用了快照器的哪些方法；
- 作为自定义快照器（懒加载、远程存储、配额等）的起点。

## 文件

| 文件 | 说明 |
|---|---|
| `main.go` | 创建 overlay 快照器，用 `snapshotservice.FromSnapshotter` 转成 gRPC 服务，监听 socket |
| `logging.go` | 装饰器：实现 `snapshots.Snapshotter` 和可选的 `snapshots.Cleaner`，转发并记录日志 |
| `deploy/config.toml` | 测试用 containerd 配置，注册 `logsnap` 代理插件 |
| `deploy/logsnap.service` | systemd 单元，保证 logsnap 先于 containerd 启动 |

## 构建

```bash
go mod tidy
CGO_ENABLED=0 go build -o logsnap .
```

已在 containerd 模块 v2.2.1 上编译通过，并在 containerd v2.4.2 守护进程上做过端到端测试。

## 快速测试（不影响系统上已有的 containerd）

```bash
# 1. 启动代理插件
sudo ./logsnap --root /tmp/logsnap-root > /tmp/logsnap.log &

# 2. 用独立目录和 socket 启动一个测试用 containerd
sudo containerd --config deploy/config.toml &

# 3. 确认插件已加载，应看到：io.containerd.snapshotter.v1  logsnap  ...  ok
sudo ctr -a /tmp/ctd-test/containerd.sock plugins ls | grep logsnap

# 4. 通过 containerd 操作快照
C="sudo ctr -a /tmp/ctd-test/containerd.sock -n demo snapshots --snapshotter logsnap"
$C prepare base-active ""
$C commit base base-active
$C prepare app-rw base
$C ls
$C mounts /mnt app-rw

# 5. 拉一个镜像试试（需要能访问镜像仓库）
sudo ctr -a /tmp/ctd-test/containerd.sock -n demo images pull --snapshotter logsnap docker.io/library/alpine:latest

# 6. 查看 logsnap 收到的调用
grep '"snapshot call"' /tmp/logsnap.log
```

## 接入正式环境

在 `/etc/containerd/config.toml` 中加入：

```toml
[proxy_plugins]
  [proxy_plugins.logsnap]
    type    = "snapshot"
    address = "/run/logsnap/logsnap.sock"
```

如果要让 Kubernetes（CRI）使用它：

```toml
[plugins."io.containerd.cri.v1.images"]
  snapshotter = "logsnap"
```

然后安装 systemd 单元，先启动 logsnap，再重启 containerd：

```bash
sudo install -m 755 logsnap /usr/local/bin/
sudo cp deploy/logsnap.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now logsnap
sudo systemctl restart containerd
```

注意：切换快照器后，已有镜像需要用新快照器重新解包（`ctr images pull --snapshotter logsnap ...` 或让 kubelet 重新拉取）。
