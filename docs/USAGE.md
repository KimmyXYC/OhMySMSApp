# 使用说明

## 1. 登录

打开 Web 页面后，使用配置文件中的用户名和密码登录。

如果前端支持可配置后端地址，可先选择或输入后端地址，再登录。

## 2. 控制面板

控制面板展示：

- 模块总数
- 在线模块数
- 已插卡模块数
- SIM 卡数量

模块卡片中可查看：

- 在线/离线状态
- 型号 / IMEI
- USB 路径
- 当前 SIM

离线模块支持删除；在线模块不能删除。

## 3. 模块页面

模块详情页可查看：

- modem 基本信息
- 当前 SIM
- 信号历史
- 网络注册状态

可执行的常见操作：

- 重置模块（如果底层支持）
- 修改备注

## 4. SIM 页面

SIM 列表展示：

- ICCID
- IMSI
- 运营商
- 号码
- 类型（pSIM / sticker eSIM / embedded eSIM）
- 当前所属模块

对于未被任何模块使用的 SIM，可执行删除。

## 5. 短信

支持：

- 查看短信线程
- 查看会话详情
- 发送短信
- 接收短信推送

短信记录会保存在数据库中。

## 6. USSD

支持发起和查看 USSD 会话。不同模块/运营商对 USSD 支持程度可能不同。

## 7. eSIM 页面

eSIM 页面可管理 sticker eSIM / eUICC 卡片与 profile。

### 页面入口能力

- 扫描发现：重新扫描 eUICC / profile
- 查看卡片详情
- 查看 profile 列表
- 修改卡片备注
- 修改 profile 昵称

### 添加 Profile

支持三种方式：

1. 直接输入 LPA 激活码
2. 手动填写 `SM-DP+` 地址和 `Matching ID`
3. 二维码扫描

如果运营商要求，还可填写 `Confirmation Code`。

### 切换 Profile

启用目标 profile 时，系统会：

1. 从芯片读取当前 profile 状态
2. 如有必要，先禁用当前 enabled profile
3. 启用目标 profile
4. 刷新 profile list
5. 自动请求重置承载该 eSIM 的 modem，帮助新 profile 生效

注意：

- eSIM 芯片状态和控制面板中的 modem 当前 SIM 状态并不总是瞬时同步
- 某些模块或固件切换生效需要几十秒
- 某些模块可能不支持标准 modem reset

### 删除 Profile

仅允许删除 disabled profile。

删除时需要输入 profile 名称进行二次确认。

## 8. Telegram

如果已配置 Telegram Bot，可用于：

- 接收短信推送
- 接收部分状态通知

具体行为以当前配置和 bot 指令实现为准。

## 9. 常见问题

### eSIM 页面显示切换成功，但控制面板还是旧运营商

说明 eUICC 已切换，但 modem/ModemManager 尚未完全更新。通常等待模块重置和重新注册后会同步。

### 连续打开 eSIM 详情会提示模块离线

当前实现对芯片读取做了缓存和恢复等待，短时间内重复查看通常不会每次都强制 live 读取。若仍偶发离线，多数是模块重枚举窗口。

### Huawei 模块提示不支持重置

多见于某些 Huawei MBIM 固件。通常不是权限问题，而是底层不支持 ModemManager 的 `Reset()` 能力。
