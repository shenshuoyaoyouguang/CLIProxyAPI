# 性能观测与回归基线

本仓库的 P3 最小闭环先聚焦在“先看清，再优化”。

## 运行时观测

- 开启 `pprof` 后，可通过 `http://127.0.0.1:8316/debug/vars` 查看 `cliproxy_runtime`
- 当前暴露的核心指标包括：
  - `watcher_backlog`
  - `refresh_queue.scheduled`
  - `refresh_queue.due`
  - `refresh_queue.inflight`
  - `active_streams`
  - `stream_first_byte_latency_ms`
  - `cancel_to_exit_latency_ms`
  - `scheduler_lock_wait_ns_by_path`

## Profile 抓取

在 `config.yaml` 中开启：

```yaml
pprof:
  enable: true
  addr: "127.0.0.1:8316"
  block-profile-rate: 1
  mutex-profile-fraction: 1
```

常用抓取命令：

```bash
go tool pprof http://127.0.0.1:8316/debug/pprof/heap
go tool pprof http://127.0.0.1:8316/debug/pprof/goroutine
go tool pprof http://127.0.0.1:8316/debug/pprof/block
go tool pprof http://127.0.0.1:8316/debug/pprof/mutex
```

建议排查顺序：

1. 先看 `/debug/vars` 判断是 watcher、refresh、scheduler 还是 stream 在放大
2. 再抓 `goroutine` 和 `heap` 看是否有堆积与泄漏迹象
3. 如果怀疑争用，再抓 `mutex` / `block`

## Benchmark 基线

新增 benchmark 重点覆盖：

- 单 provider 的 `pick + mark` 并发路径
- mixed provider 的 `pick + mark` 并发路径
- refresh 调度识别路径

推荐命令：

```bash
go test ./sdk/cliproxy/auth -run '^$' -bench 'BenchmarkManager' -benchmem
```
