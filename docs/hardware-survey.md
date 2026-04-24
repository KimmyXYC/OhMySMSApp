# 硬件勘察报告

> 日期：2026-04-24  
> 目标设备：树莓派 5（172.16.84.233, kimmy@）  
> OS：Debian 13 (trixie) / Kernel 6.12.75 / aarch64 / Cortex-A76  
> ModemManager 1.24.0 · NetworkManager 1.52.1

## 模块 & SIM 一览

> **注意**：MM DBus 的 `/Modem/<N>` 路径每次 MM 重启都会变，`primary_port` 的 `cdc-wdm<N>` 也会随 USB 枚举顺序变。唯一稳定的标识是 `Modem.DeviceIdentifier`（MM 生成的 hash）与 IMEI。本项目用前者做 upsert key。

### 硬件变更记录
- 2026-04-24 初次勘察：5 个模块（含 2 个空卡槽），其中 Modem37（IMEI 863418059992990）故障，已**物理拆除**
- 当前：**4 个模块**在线

### 当前布局

| IMEI | 型号 | 固件 | 卡槽 | SIM 类型 | 运营商/号码 |
|---|---|---|---|---|---|
| 863418059993014 | Quectel EC20-CE | `EC20CEHCR06A03M1G` | 已插卡 | **pSIM** | CHN-CT (45403) |
| 863418059993139 | Quectel EC20-CE | `EC20CEHCR06A04M1G` | 已插卡 | **5ber sticker eSIM** | giffgaff UK，漫游 CMCC |
| 867223020359329 | Huawei ME906s (V7R11) | `11.617.06.demo.00` | 已插卡 | **9eSIM sticker eSIM** | 德国 262，+491791566795，漫游 CMCC |
| 863418059993006 | Quectel EC20-CE | `EC20CEHCR06A03M1G` | 空 | - | - |

## 各 SIM 详情（快照，2026-04-24）

| 对应模块 IMEI | Kind | ICCID | IMSI | 运营商 | 号码 | 注册状态 |
|---|---|---|---|---|---|---|
| 863418059993014 | pSIM | `8985203105011606981` | 454031051160698 | CHN-CT (45403) | - | roaming |
| 863418059993139 | 5ber sticker eSIM | `8944110069156835483` | 234104846999661 | giffgaff (23410) | - | roaming 在 CMCC |
| 867223020359329 | 9eSIM sticker eSIM | 待 lpac 读取 | 262036013169687 | 262 (德国) | `+491791566795` | roaming 在 CMCC |

## eSIM 切换能力探测

### Quectel EC20F（Modem 33/35/37）
- **不支持** `AT+CCHO` / `AT+CCHC` / `AT+CGLA` / `AT+CRLA` → 标准 LPA（SGP.22 APDU 逻辑通道）**走 AT 不通**
- **不支持** `AT+CUSAT*` / `AT+QSTK*` → STK 菜单接入不可用
- **支持** `AT+CRSM`（受限 SIM 访问）和基础 3GPP AT 指令
- **关键结论**：EC20F 上做 eSIM 切换**必须通过 QMI UIM 服务**（绕过 AT），已有实测案例：  
  <https://qiedd.com/2015.html>（EC20-CE 实测 `LPAC_APDU=qmi QMI_DEVICE=/dev/cdc-wdm0 lpac chip info` 成功）
- 前置：`AT+QCFG="usbnet",0` 确保 QMI 模式（MBIM 模式下 lpac 的 MBIM 驱动在 EC20F 上报 `NoDeviceSupport`）

### Huawei ME906s（Modem 36）
- 纯 MBIM（无 AT TTY），标准 UICC APDU 通道走 MBIM
- lpac 的 MBIM 驱动应可工作，现场需 `lpac chip info` 验证

## eSIM 切换方案（确定路径）

**核心策略：复用开源 lpac 二进制（AGPL，独立进程 + JSON IPC）**

- **仓库**：<https://github.com/estkme-group/lpac>
- **驱动**：
  - 5ber（Modem 35）：`LPAC_APDU=qmi QMI_DEVICE=/dev/cdc-wdm3 lpac -a <AID_5BER> profile enable <ICCID>`
  - 9eSIM（Modem 36）：`LPAC_APDU=mbim MBIM_DEVICE=/dev/cdc-wdm4 lpac profile enable <ICCID>`
- **AID**：5ber 使用自定义 ISD-R AID（EasyLPAC 源码 `AID_5BER` 常量），9eSIM 使用默认 AID
- **EID 前缀识别**：9eSIM = `89044045...`（5ber 运行时读取后缓存）
- **MM 互斥**：lpac 操作前 `mmcli -m <X> --inhibit`，结束后解除，防止 UIM 服务抢占和 SIM refresh 竞争

## 影响本工具的设计约束

1. **模块能力分级**：
   - EC20F：短信、USSD、信号、IMEI、ICCID 全功能；eSIM 走 QMI LPA
   - ME906s：短信、USSD、信号、IMEI、ICCID 全功能（经 MM/DBus）；eSIM 走 MBIM LPA
2. **AT 直通仅作为兜底**：主通道走 ModemManager DBus；私有 AT 需要时通过 `mmcli --command` + MM debug 模式，或手动释放 TTY
3. **空卡槽显示**：Modem 32/37 保留在模块列表中（支持后续热插拔上卡）
4. **eSIM 架构抽象**（按 transport 分治，非按 vendor）：
   - `ApduTransport` interface：QMI / MBIM / PCSC
   - `CardVendor` metadata：FiveBer / NineESIM / Unknown（只含 AID + 展示名）

## 已安装/待安装工具

| 工具 | 状态 | 用途 |
|---|---|---|
| ModemManager | ✅ 1.24.0 | 主控制层 |
| NetworkManager | ✅ 1.52.1 | 连接管理 |
| socat | ✅ | 串口调试 |
| python3-serial | ✅ | AT 探测脚本 |
| lpac | ❌ 待编译 | eSIM profile 管理 |
| libqmi-utils | ⚠️ 需确认 | QMI 客户端 |
| libmbim-utils | ⚠️ 需确认 | MBIM 客户端 |

## 参考

- lpac：<https://github.com/estkme-group/lpac>
- lpac USAGE：<https://github.com/estkme-group/lpac/blob/main/docs/USAGE.md>
- EasyLPAC（含 AID_5BER）：<https://github.com/creamlike1024/EasyLPAC>
- 9eSIM 官方工具页：<https://dl.9esim.com/>
- Quectel EC20 实测 lpac QMI：<https://qiedd.com/2015.html>
- 9eSIM Linux 实测：<https://neilzone.co.uk/2025/01/using-esims-with-devices-that-only-have-a-physical-sim-slot-via-a-9esim-sim-card-with-android-and-linux/>
