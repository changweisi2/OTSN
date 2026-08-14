# dwatch — disk usage watcher

追踪磁盘空间变化：**什么时候、在哪个路径、涨了多少**。一个静态二进制，运行于 Linux / macOS / Windows。

```console
$ dwatch watch --interval 10m /home/cat
◈ dwatch watching /home/cat · every 10m0s · event-triggered
10:32:01 Δ +412 MB  ·  1,204 files changed  ·  top: ~/.cache (+380 MB)
10:42:01 Δ +12 MB   ·  84 files changed     ·  top: ~/Downloads (+9 MB)
▲ 11:02:01 Δ +1.8 GB  ·  3,120 files changed  ·  top: ~/vmdisk (+1.7 GB)  ▲ growth ≥ 1 GB
```

## 特性

- **全平台**：Go 单二进制，`CGO_ENABLED=0` 交叉编译，无运行时依赖。
- **低占用**：常驻内存 ~20–40 MB；扫描为并发元数据遍历（只 Lstat，不读文件内容），整盘重扫通常数秒，且只在扫描窗口内占用 CPU。
- **双引擎**：
  - **扫描引擎**（主力）：并发遍历整棵树，快照存 gzip+gob，跨平台行为一致，结果始终准确；
  - **事件引擎**（辅助）：fsnotify 监听（Linux inotify / macOS kqueue / Windows ReadDirectoryChangesW），有写入事件时提前触发扫描，缩短发现延迟。
- **时间线回溯**：快照归档 + 保留策略（默认保留最近 48 个），随时回答"从什么时候开始涨的"。
- **机器可读**：所有命令支持 `--json`，便于接入脚本与告警。

## 安装

```console
$ make build                # bin/dwatch
$ make release              # 交叉编译 6 个目标到 dist/
```

或直接：

```console
$ go build -o dwatch .
```

## 快速开始

```console
# 1. 基线快照（默认整盘；可指定目录）
$ dwatch scan /home/cat

# 2. 常驻监听（Ctrl-C 退出；建议配合 systemd / launchd / 任务计划）
$ dwatch watch --interval 10m --alert 1024 /home/cat

# 3. 查看两次快照之间的增长明细
$ dwatch report

# 4. 当前占用排行
$ dwatch top --depth 2
```

## 命令

| 命令 | 说明 |
|---|---|
| `scan` | 扫描并存储快照（默认整盘；`--exclude` 跳过前缀） |
| `watch` | 周期扫描 + 事件触发；`--interval`、`--alert`（增长告警 MB） |
| `serve` | 本地 Web 仪表盘（默认 http://127.0.0.1:8787） |
| `report` | 两次快照 diff：`--since`（序号或时间）、`--depth`、`--min`、`--all`（全部区间） |
| `top` | 最新快照占用排行：`--depth`、`--n` |
| `list` / `prune` | 快照归档管理：`--keep` |

所有命令支持 `--json`。`NO_COLOR` 环境变量或非 TTY 下自动禁用颜色。

## Web 仪表盘

`dwatch serve` 在本地起一个极简 Web 界面（深色 + 青蓝风格，无第三方依赖，10 秒自动刷新）：

```console
$ dwatch serve --interval 5m /home/cat
◈ dwatch dashboard at http://127.0.0.1:8787 · watching /home/cat · every 5m0s
```

浏览器打开 http://127.0.0.1:8787：

- **状态卡片**：总量、上次变化 Δ、条目数、快照数；
- **时间线**：SVG 折线图展示总量随历史快照的变化趋势；
- **TOP 排行**：当前占用最大的目录（深度可调）；
- **增长明细**：最近两次快照间的变化表，精确到路径和字节。

页面数据来自 `http://127.0.0.1:8787/api/{status,top,history,report}`（JSON），可自行接入其他工具。监听地址用 `--addr` 调整（默认仅绑定本机 127.0.0.1）。

## 工作原理

```
┌─ 扫描引擎（跨平台统一）────────────────────────────┐
│ 每轮：并发遍历整棵树（Lstat 元数据，不读文件内容）     │
│  ├ 目录只读 dirent，文件只读 size + mtime             │
│  ├ 不跟随符号链接（防环）；不可读路径跳过并计数        │
│  └ 快照（gzip+gob，按路径排序）写入归档，保留最近 48 个 │
└──────────────────────────────────────────────────┘
┌─ 事件引擎（按平台最轻实现）────────────────────────┐
│ 根目录挂 fsnotify watcher → 任意事件触发提前扫描      │
│ （触发仅作"脏标记"，扫描本身保持并发全量，准确优先）    │
└──────────────────────────────────────────────────┘
```

快照存储位置：`$DWATCH_DIR`（默认 `<用户配置目录>/dwatch/snapshots`，Linux `~/.config/dwatch`，macOS `~/Library/Application Support/dwatch`，Windows `%AppData%\dwatch`）。

### 为什么不做"增量扫描"？

目录 mtime 只在**该目录的直接子项**增删改时更新；文件原地增长（append）和深层变化不会更新任何祖先目录的 mtime。因此"目录 mtime 短路"必然漏检，dwatch 选择**每轮全量并发遍历**换取结果的绝对可靠。作为补偿：

- 扫描只读元数据（Lstat），不读文件内容，整盘秒级完成；
- 事件引擎提前触发，发现延迟 ≈ 事件发生到下一轮扫描的间隔；
- 快照之间用文件级 size diff 报告变化，精确到字节。

## 性能

| 场景 | 预期 |
|---|---|
| 全盘扫描（百万级文件） | 数秒 ~ 数十秒，取决于磁盘 IO |
| 常驻内存 | ~20–40 MB |
| 快照文件 | 约 5–15 MB / 百万文件（gzip 后） |

## 开发

```console
$ make test     # go test ./...
$ make race     # go test -race ./...
$ make vet
$ make lint     # 可选 golangci-lint
```

架构：`main.go`（入口）→ `internal/app`（子命令）→ `internal/snapshot`（扫描/快照/diff）、`internal/store`（归档）、`internal/events`（事件触发）、`internal/ui`（终端渲染）。仅一个运行时依赖：`github.com/fsnotify/fsnotify`。
