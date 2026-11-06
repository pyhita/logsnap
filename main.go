// logsnap 是一个最小的 containerd 代理快照器（proxy snapshotter）示例。
//
// 它在一个 unix socket 上提供 containerd 的 Snapshots gRPC API，
// 内部把真正的工作交给 overlayfs 快照器，同时记录每次调用的
// namespace、参数、耗时和错误，方便观察 containerd 在拉镜像、
// 起容器时到底调用了快照器的哪些方法。
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/v2/contrib/snapshotservice"
	"github.com/containerd/containerd/v2/plugins/snapshots/overlay"
	"google.golang.org/grpc"
)

func main() {
	var (
		address = flag.String("address", "/run/logsnap/logsnap.sock", "gRPC 监听的 unix socket")
		root    = flag.String("root", "/var/lib/logsnap", "快照数据目录")
	)
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// 1. 创建真正干活的快照器。这里直接复用 containerd 自带的 overlayfs 实现，
	//    它会在 root 下维护自己的 metadata.db 和 snapshots/ 目录。
	inner, err := overlay.NewSnapshotter(*root)
	if err != nil {
		log.Error("create overlay snapshotter", "err", err)
		os.Exit(1)
	}

	// 2. 包一层日志装饰器。
	sn := &loggingSnapshotter{inner: inner, log: log}

	// 3. 把 snapshots.Snapshotter 接口适配成 gRPC 的 SnapshotsServer，
	//    并注册到 gRPC server 上。这一步就是"成为代理插件"的全部要求。
	srv := grpc.NewServer()
	snapshotsapi.RegisterSnapshotsServer(srv, snapshotservice.FromSnapshotter(sn))

	// 4. 监听 unix socket。
	if err := os.MkdirAll(filepath.Dir(*address), 0o711); err != nil {
		log.Error("mkdir socket dir", "err", err)
		os.Exit(1)
	}
	_ = os.Remove(*address) // 清理上次残留的 socket
	l, err := net.Listen("unix", *address)
	if err != nil {
		log.Error("listen", "address", *address, "err", err)
		os.Exit(1)
	}

	// 5. 收到退出信号时优雅关闭。
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		log.Info("shutting down")
		srv.GracefulStop()
	}()

	log.Info("logsnap serving", "address", *address, "root", *root)
	if err := srv.Serve(l); err != nil {
		log.Error("serve", "err", err)
	}
	if err := sn.Close(); err != nil {
		log.Error("close snapshotter", "err", err)
	}
}
