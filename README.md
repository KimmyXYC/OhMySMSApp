# ohmysmsapp

> SMS 管理工具 · Go + Vue3 + Telegram Bot · 跑在树莓派 + 多 4G 模块

## 架构

```
┌──────────── 开发机 ────────────┐          ┌──────── 树莓派 5 (172.16.84.233) ────────┐
│  monorepo / 交叉编译 / 部署     │──rsync──▶│  /opt/ohmysmsapp/                         │
│  Vue dev server (开发态)        │          │    ohmysmsd (Go, aarch64)                 │
└────────────────────────────────┘          │    web/ (Vue dist，静态由 Go 托管)         │
                                            │    data/ohmysmsapp.db (SQLite)            │
                                            │                                           │
                                            │  ModemManager (DBus) ─┬─ 5x 4G modules    │
                                            │  lpac (eSIM LPA)     ─┘  Quectel EC20F×4  │
                                            │                          Huawei  ME906s×1 │
                                            └───────────────────────────────────────────┘
```

## 技术栈

- **后端**：Go 1.22+ · chi · godbus/dbus · modernc.org/sqlite · coder/websocket · JWT
- **前端**：Vue 3 · Vite · TypeScript · Pinia · Element Plus
- **Bot**：go-telegram-bot-api/v5
- **eSIM**：[lpac](https://github.com/estkme-group/lpac)（QMI/MBIM APDU 驱动）
- **部署**：systemd unit · rsync + ssh

## 目录结构

```
.
├── backend/                  Go 后端
│   ├── cmd/ohmysmsd/          可执行入口
│   ├── internal/
│   │   ├── config/            配置加载
│   │   ├── db/                SQLite / migrations
│   │   ├── modem/             ModemManager DBus 客户端
│   │   ├── sms/               短信收发
│   │   ├── ussd/              USSD
│   │   ├── esim/              lpac 封装 + provider 抽象
│   │   ├── telegram/          TG bot
│   │   ├── httpapi/           REST 路由
│   │   ├── ws/                WebSocket 推送
│   │   ├── auth/              JWT + bcrypt
│   │   └── logging/           slog 封装
│   ├── migrations/            SQL 迁移脚本
│   └── pkg/                   可导出的公共包
├── frontend/                 Vue3 前端
├── bot/                      （未来若独立进程化 TG bot）
├── deploy/
│   ├── systemd/               .service 单元
│   └── scripts/               部署/回滚脚本
├── docs/                     设计文档、勘察报告
└── scripts/                  本地开发脚本
```

## 路线图

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 硬件勘察 | ✅ `docs/hardware-survey.md` |
| 1 | 项目骨架 + 部署链路 | 🚧 in progress |
| 2 | ModemManager DBus 集成（模块/SIM/信号/IMEI） |  |
| 3 | 短信/USSD + HTTP API + WebSocket |  |
| 4 | Vue 前端 |  |
| 5 | Telegram Bot |  |
| 6 | lpac 集成 + eSIM profile 切换 |  |
| 7 | 打磨（信号历史、审计、错误恢复） |  |

## 开发

```bash
# 后端本地跑（会连树莓派的 MM？不，本地跑用 mock；真机跑部署到树莓派）
make backend-run-local

# 交叉编译 aarch64 并部署到树莓派
make deploy

# 前端开发
make frontend-dev
```

## 设备清单

参见 [docs/hardware-survey.md](docs/hardware-survey.md)。
