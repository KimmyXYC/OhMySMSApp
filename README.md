# ohmysmsapp

基于 **Go + Vue 3 + ModemManager + Telegram Bot + lpac** 的短信与 eSIM 管理工具。

适用于通过 **ModemManager(DBus)** 管理多块 4G/5G 模块的 Linux 主机。项目默认面向单机部署，
后端负责短信/USSD/eSIM/profile 管理，前端提供 Web 管理界面。

## 功能概览

- 多模块状态、SIM、信号查看
- 短信收发、线程列表、历史保存
- USSD 会话
- Telegram Bot 推送与控制
- 基于 `lpac` 的 sticker eSIM profile 管理：
  - 扫描发现 eUICC / profile
  - 添加 profile（LPA 激活码、手动 SM-DP+ / Matching ID、二维码）
  - 启用 / 禁用 / 删除 profile
  - 切换后自动请求重置目标 modem，帮助新 profile 生效

## 技术栈

- 后端：Go 1.22+
- 前端：Vue 3 / Vite / TypeScript / Pinia / Element Plus
- 通信：HTTP API + WebSocket
- 数据库：SQLite
- 模块控制：ModemManager DBus
- eSIM：lpac

## 仓库结构

```text
.
├── backend/                  后端服务
│   ├── cmd/ohmysmsd/         程序入口
│   └── internal/
│       ├── auth/             JWT / 密码
│       ├── config/           配置加载与校验
│       ├── db/               SQLite / migrations
│       ├── esim/             lpac 封装与 eSIM 逻辑
│       ├── httpapi/          HTTP API
│       ├── modem/            ModemManager DBus 集成
│       ├── telegram/         Telegram Bot
│       └── ws/               WebSocket 推送
├── frontend/                 Vue 前端
├── deploy/                   部署模板（systemd 等）
├── docs/                     设计/调研文档
├── scripts/                  辅助脚本
└── Makefile                  本地构建/部署入口
```

## 快速开始

### 1. 本地开发

后端本地开发默认可配合 mock modem provider 运行，适合调 UI / API。

```bash
# 安装前端依赖
make frontend-install

# 启动前端开发服务器
make frontend-dev

# 本地运行后端（需准备 backend/config.yaml）
make backend-run-local
```

### 2. 构建

```bash
# 构建本机后端二进制
make build

# 交叉编译 Linux/arm64（可覆盖 GOOS/GOARCH）
make build-linux-arm64

# 构建前端产物
make frontend-build
```

## 配置

配置样例位于：

- `backend/config.example.yaml`

复制后按需修改：

```bash
cp backend/config.example.yaml backend/config.yaml
```

重点配置项：

- `server.listen`：监听地址
- `database.path`：SQLite 路径
- `auth.jwt_secret`：生产环境必须设置
- `auth.password_bcrypt`：生产环境必须设置
- `modem.enabled`：是否启用 ModemManager
- `telegram.bot_token` / `telegram.chat_id`
- `esim.lpac_bin` / `esim.lpac_drivers_dir`

### 生产环境安全要求

生产环境至少要做：

1. 设置随机高强度 `auth.jwt_secret`
2. 设置 `auth.password_bcrypt`
3. 改掉默认用户名 `admin`
4. 限制监听地址、反向代理或内网访问范围
5. 不要把真实 `config.yaml`、数据库、日志、`.git/` 打包分发

## 部署

详细步骤见：

- [deploy/README.md](deploy/README.md)

该文档包含：

- 构建与打包
- 配置文件准备
- systemd 部署
- lpac 安装方式
- 常见排障

## 使用说明

详细使用说明见：

- [docs/USAGE.md](docs/USAGE.md)

包含：

- 登录与后端切换
- 模块与 SIM 页面
- 短信/USSD
- eSIM profile 扫描/添加/切换/删除
- Telegram 配置

## Makefile 说明

Makefile 已尽量通用化，部署目标通过变量传入，例如：

```bash
make deploy \
  DEPLOY_HOST=user@example-host \
  DEPLOY_DIR=/srv/ohmysmsapp \
  DEPLOY_SERVICE=ohmysmsd \
  TARGET_GOOS=linux \
  TARGET_GOARCH=arm64
```

常用目标：

- `make build`
- `make build-linux-arm64`
- `make frontend-build`
- `make test`
- `make lint`
- `make deploy`
- `make remote-status`
- `make remote-logs`

## 测试

```bash
cd backend && go test ./...
cd frontend && npm run build
```

## 注意事项

- eSIM profile 状态与 modem 当前识别到的 SIM 状态并不总是瞬时一致
- 切换 profile 后，系统会尝试让目标 modem 重新识别新 profile
- 某些模块/固件可能不支持标准 `Modem.Reset()`，需要等待、重新接管或手动恢复
- sticker eSIM、MBIM/QMI、模块固件差异会显著影响切换体验

## 许可证

项目自身许可证以仓库实际声明为准。若使用 `lpac`，请同时注意其上游许可证要求。
