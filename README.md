# otsn — disk usage tracker

追踪磁盘空间变化：**什么时候、在哪个路径、涨了多少**。一个静态二进制，运行于 Linux / macOS / Windows。

```console
$ otsn scan /home/cat
◈ scan complete
  paths      /home/cat
  disk       34.3 GB used of 207.0 GB (16.6%) · 172.7 GB free
  file size  46.3 MB
  Δ          +12.4 MB since 2026-08-14 20:14  (84 files changed)

$ otsn serve
◈ otsn dashboard at http://127.0.0.1:8787 · serving stored snapshots
```

## 特性

- **全平台**：Go 单二进制，`CGO_ENABLED=0` 交叉编译，无运行时依赖。
- **低占用**：扫描为并发元数据遍历（只 Lstat，不读文件内容），整盘重扫通常数秒；`serve` 只读快照，常驻内存 ~20–40 MB。
- **手动触发**：`otsn scan` 决定何时产生数据——需要时扫描一次，配 cron / launchd / 任务计划即可定时。
- **时间线回溯**：快照归档 + 保留策略（默认保留最近 48 个），随时回答"从什么时候开始涨的"。
- **磁盘占用双口径**：磁盘真实占用（df，分区合计）与文件大小总和（扫描）分开展示。
- **机器可读**：所有命令支持 `--json`，便于接入脚本与告警。

## 安装

```console
$ make build                # bin/otsn
$ make release              # 交叉编译 6 个目标到 dist/
```

或直接：

```console
$ go build -o otsn .
```

## 快速开始

```console
# 1. 建立基线快照（默认整盘；可指定目录）
$ otsn scan /home/cat

# 2. 再次扫描 → 显示与上次的 Δ 变化
$ otsn scan /home/cat

# 3. 查看两次快照之间的增长明细
$ otsn report

# 4. 当前占用排行
$ otsn top --depth 2

# 5. Web 仪表盘（只读展示，数据来自 scan）
$ otsn serve
```

想定时自动扫描？用系统的计划任务即可：

```console
$ crontab -e          # 每 10 分钟扫一次
*/10 * * * * otsn scan
```

## 命令

| 命令 | 说明 |
|---|---|
| `scan` | 扫描并存储快照（默认整盘；`--exclude` 跳过前缀） |
| `serve` | 本地 Web 仪表盘（默认 http://127.0.0.1:8787，只读展示） |
| `report` | 两次快照 diff：`--since`（序号或时间）、`--depth`、`--min`、`--all`（全部区间） |
| `top` | 最新快照占用排行：`--depth`、`--n` |
| `list` / `prune` | 快照归档管理：`--keep` |

所有命令支持 `--json`。`NO_COLOR` 环境变量或非 TTY 下自动禁用颜色。

## Web 仪表盘

`otsn serve` 在本地起一个单页 Web 仪表盘（浅色界面 + 淡蓝点缀，零第三方依赖，10 秒自动刷新）：

```console
$ otsn serve
◈ otsn dashboard at http://127.0.0.1:8787 · serving stored snapshots
```

浏览器打开 http://127.0.0.1:8787：

- **环形仪表**：磁盘占用率（已用/总量，df 口径），渐变进度环；
- **指标卡**：磁盘总量、磁盘剩余、文件大小总和、上次变化 Δ、条目数、快照数；
- **时间线**：磁盘占用随时间的变化曲线，hover 显示十字准线与数据点详情；
- **TOP 排行**：当前占用最大的目录（渐变条形图）；
- **增长明细**：最近两次快照间的变化表，精确到路径和字节。

页面数据来自 `http://127.0.0.1:8787/api/{status,top,history,report}`（JSON），可自行接入其他工具。监听地址用 `--addr` 调整（默认仅绑定本机 127.0.0.1）。

## 工作原理

```
┌─ scan（跨平台统一，手动触发）─────────────────────┐
│ 并发遍历整棵树（Lstat 元数据，不读文件内容）           │
│  ├ 目录只读 dirent，文件只读 size + mtime             │
│  ├ 不跟随符号链接（防环）；不可读路径跳过并计数        │
│  ├ 磁盘占用（df 口径）与文件大小同时记录               │
│  └ 快照（gzip+gob，按路径排序）写入归档，保留最近 48 个 │
└──────────────────────────────────────────────────┘
┌─ serve（只读展示）────────────────────────────────┐
│ 读归档快照 → 内存渲染 API → 浏览器轮询刷新            │
│ 不做任何扫描，数据只来自 otsn scan                    │
└──────────────────────────────────────────────────┘
```

快照存储位置：`$OTSN_DIR`（默认 `<用户配置目录>/otsn/snapshots`，Linux `~/.config/otsn`，macOS `~/Library/Application Support/otsn`，Windows `%AppData%\otsn`）。

### 为什么不做"增量扫描"？

目录 mtime 只在**该目录的直接子项**增删改时更新；文件原地增长（append）和深层变化不会更新任何祖先目录的 mtime。因此"目录 mtime 短路"必然漏检，otsn 选择**每轮全量并发遍历**换取结果的绝对可靠。作为补偿：

- 扫描只读元数据（Lstat），不读文件内容，整盘秒级完成；
- 快照之间用文件级 size diff 报告变化，精确到字节。

## 性能

| 场景 | 预期 |
|---|---|
| 全盘扫描（百万级文件） | 数秒 ~ 数十秒，取决于磁盘 IO |
| serve 常驻内存 | ~20–40 MB |
| 快照文件 | 约 5–15 MB / 百万文件（gzip 后） |

## 开发

```console
$ make test     # go test ./...
$ make race     # go test -race ./...
$ make vet
$ make lint     # 可选 golangci-lint
```

架构：`main.go`（入口）→ `internal/app`（子命令）→ `internal/snapshot`（扫描/快照/diff）、`internal/store`（归档）、`internal/ui`（终端渲染）。仅一个平台依赖：`golang.org/x/sys`（Windows 磁盘枚举与 VT 终端）。
