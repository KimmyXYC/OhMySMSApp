# 部署说明

本文档说明如何将 ohmysmsapp 部署到一台安装了 ModemManager 的 Linux 主机。

## 1. 前置条件

目标主机需要具备：

- Linux（建议 systemd 环境）
- ModemManager
- 至少一块受支持的 4G/5G modem
- SQLite 可写目录
- 如需 eSIM：`lpac` 及其依赖库/驱动

构建机需要具备：

- Go 1.22+
- Node.js 20+
- npm
- `ssh` / `rsync`

## 2. 构建

### 后端

```bash
make build
```

交叉编译示例：

```bash
make build-linux-arm64 TARGET_GOOS=linux TARGET_GOARCH=arm64
```

### 前端

```bash
make frontend-build
```

构建产物默认输出到：

- 后端：`deploy/build/`
- 前端：`deploy/build/web/`

## 3. 准备配置文件

复制样例配置：

```bash
cp backend/config.example.yaml config.yaml
```

最少修改：

- `auth.jwt_secret`
- `auth.password_bcrypt`
- `database.path`
- `server.listen`
- `esim.lpac_bin`
- `esim.lpac_drivers_dir`（若需要）

生成密码 hash：

```bash
./scripts/hash-password.sh
```

或使用程序子命令（如果已提供）。

## 4. 目标目录布局（示例）

```text
/srv/ohmysmsapp/
├── bin/
│   └── ohmysmsd
├── web/
├── data/
├── config.yaml
└── config.example.yaml
```

> 也可以使用 `/opt/ohmysmsapp` 或其他目录，本文以 `/srv/ohmysmsapp` 作为通用示例。

## 5. systemd 部署

仓库中提供了模板：

- `deploy/systemd/ohmysmsd.service`

部署前请至少修改：

- `WorkingDirectory`
- `ExecStart`
- `ReadWritePaths`
- `User` / `Group`
- `DeviceAllow`（按实际硬件节点调整）

安装示例：

```bash
sudo cp deploy/systemd/ohmysmsd.service /etc/systemd/system/ohmysmsd.service
sudo systemctl daemon-reload
sudo systemctl enable ohmysmsd
sudo systemctl restart ohmysmsd
sudo systemctl status ohmysmsd
```

日志查看：

```bash
sudo journalctl -u ohmysmsd -f
```

## 6. lpac 安装

如果使用 eSIM，需要安装：

- `lpac` 可执行文件
- 相关 `.so`
- APDU 驱动

配置项：

- `esim.lpac_bin`
- `esim.lpac_drivers_dir`

如果 lpac 已在 PATH 中、依赖也已通过系统库路径安装，可以仅设置：

```yaml
esim:
  lpac_bin: "lpac"
  lpac_drivers_dir: ""
```

## 7. 使用 Makefile 部署

Makefile 支持通过变量指定目标主机和安装目录：

```bash
make deploy \
  DEPLOY_HOST=user@example-host \
  DEPLOY_DIR=/srv/ohmysmsapp \
  DEPLOY_SERVICE=ohmysmsd \
  TARGET_GOOS=linux \
  TARGET_GOARCH=arm64
```

辅助命令：

```bash
make remote-status DEPLOY_HOST=user@example-host DEPLOY_SERVICE=ohmysmsd
make remote-logs   DEPLOY_HOST=user@example-host DEPLOY_SERVICE=ohmysmsd
```

## 8. 反向代理（可选）

如果要暴露到内网/公网，建议放在反向代理后，并限制访问范围。

建议至少做：

- HTTPS
- Basic Auth / 内网访问控制 / Zero Trust
- 限制来源 IP

## 9. 常见问题

### eSIM 页面显示已切换，但控制面板仍是旧 SIM

说明 eUICC profile 状态和 ModemManager 当前识别到的 SIM 状态还未完全同步。先等待模块重新接管；如果仍不同步，再检查 modem reset / MM 事件。

### 某些模块提示不支持重置

常见于部分 Huawei MBIM 固件。通常不是权限问题，而是底层不支持 ModemManager 可调用的 `Reset()` 能力。

### `modem currently offline`

可能出现在：

- inhibit/uninhibit 恢复窗口内
- 模块正在重枚举
- ModemManager 尚未重新接管

建议先等待几十秒，再查看 `journalctl` 和 `/api/modems`。

## 10. 升级建议

升级时建议保留：

- `config.yaml`
- `data/`

并备份：

- 当前二进制
- 当前 web 目录
- 数据库文件
