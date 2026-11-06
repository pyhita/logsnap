package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

// loggingSnapshotter 实现 snapshots.Snapshotter 接口：
// 每个方法都转发给 inner，并打印一条结构化日志。
type loggingSnapshotter struct {
	inner snapshots.Snapshotter
	log   *slog.Logger
}

// 编译期确认实现了需要的接口。
var (
	_ snapshots.Snapshotter = (*loggingSnapshotter)(nil)
	_ snapshots.Cleaner     = (*loggingSnapshotter)(nil)
)

// record 打印一次调用的结果。namespace 来自 containerd 通过 gRPC metadata
// 头 "containerd-namespace" 传过来的值，namespaces 包会自动解析。
func (s *loggingSnapshotter) record(ctx context.Context, op string, start time.Time, err error, attrs ...any) {
	ns, _ := namespaces.Namespace(ctx)
	attrs = append(attrs, "op", op, "namespace", ns, "took", time.Since(start).String())
	if err != nil {
		s.log.Warn("snapshot call failed", append(attrs, "err", err)...)
		return
	}
	s.log.Info("snapshot call", attrs...)
}

func (s *loggingSnapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	start := time.Now()
	info, err := s.inner.Stat(ctx, key)
	s.record(ctx, "Stat", start, err, "key", key)
	return info, err
}

func (s *loggingSnapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	start := time.Now()
	out, err := s.inner.Update(ctx, info, fieldpaths...)
	s.record(ctx, "Update", start, err, "key", info.Name, "fields", fieldpaths)
	return out, err
}

func (s *loggingSnapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	start := time.Now()
	u, err := s.inner.Usage(ctx, key)
	s.record(ctx, "Usage", start, err, "key", key, "bytes", u.Size)
	return u, err
}

func (s *loggingSnapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	start := time.Now()
	m, err := s.inner.Mounts(ctx, key)
	s.record(ctx, "Mounts", start, err, "key", key, "mounts", len(m))
	return m, err
}

func (s *loggingSnapshotter) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	start := time.Now()
	m, err := s.inner.Prepare(ctx, key, parent, opts...)
	s.record(ctx, "Prepare", start, err, "key", key, "parent", parent, "mounts", describe(m))
	return m, err
}

func (s *loggingSnapshotter) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	start := time.Now()
	m, err := s.inner.View(ctx, key, parent, opts...)
	s.record(ctx, "View", start, err, "key", key, "parent", parent, "mounts", describe(m))
	return m, err
}

func (s *loggingSnapshotter) Commit(ctx context.Context, name, key string, opts ...snapshots.Opt) error {
	start := time.Now()
	err := s.inner.Commit(ctx, name, key, opts...)
	s.record(ctx, "Commit", start, err, "name", name, "key", key)
	return err
}

func (s *loggingSnapshotter) Remove(ctx context.Context, key string) error {
	start := time.Now()
	err := s.inner.Remove(ctx, key)
	s.record(ctx, "Remove", start, err, "key", key)
	return err
}

func (s *loggingSnapshotter) Walk(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	start := time.Now()
	n := 0
	err := s.inner.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
		n++
		return fn(ctx, info)
	}, filters...)
	s.record(ctx, "Walk", start, err, "filters", filters, "count", n)
	return err
}

// Cleanup 是可选接口：containerd GC 之后会调用它，让快照器清理磁盘上的残留目录。
func (s *loggingSnapshotter) Cleanup(ctx context.Context) error {
	c, ok := s.inner.(snapshots.Cleaner)
	if !ok {
		return nil
	}
	start := time.Now()
	err := c.Cleanup(ctx)
	s.record(ctx, "Cleanup", start, err)
	return err
}

func (s *loggingSnapshotter) Close() error {
	return s.inner.Close()
}

// describe 把挂载列表压缩成便于阅读的字符串，例如 "overlay(lowerdir=...)"。
func describe(ms []mount.Mount) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Type+"("+m.Source+")")
	}
	return out
}
