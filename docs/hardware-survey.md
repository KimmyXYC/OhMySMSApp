# 硬件勘察报告

> 日期：2026-04-24  
> 目标设备：树莓派 5（172.16.84.233, kimmy@）  
> OS：Debian 13 (trixie) / Kernel 6.12.75 / aarch64 / Cortex-A76  
> ModemManager 1.24.0 · NetworkManager 1.52.1

## 模块 & SIM 一览

| Modem ID | 型号 | 固件 | IMEI | USB Path | AT 端口 | QMI/MBIM | 卡槽 |
|---|---|---|---|---|---|---|---|
| 32 | Quectel EC20-CE (EC20F) | `EC20CEHCR06A03M1G` | 863418059993006 | `1-1.2` | ttyUSB6/7 | `/dev/cdc-wdm1` (QMI) | **空** |
| 33 | Quectel EC20-CE (EC20F) | `EC20CEHCR06A03M1G` | 863418059993014 | `1-1.1` | ttyUSB2/3 | `/dev/cdc-wdm0` (QMI) | **pSIM**（实体卡） |
| 35 | Quectel EC20-CE (EC20F) | `EC20CEHCR06A04M1G` | 863418059993139 | `1-1.4/1-1.4.2` | ttyUSB14/15 | `/dev/cdc-wdm3` (QMI) | **5ber.eSIM** |
| 36 | Huawei ME906s (V7R11) | `11.617.06.demo.00` | 867223020359329 | `1-1.4/1-1.4.4/1-1.4.4.2` | 无 TTY（纯 MBIM） | `/dev/cdc-wdm4` (MBIM) | **9eSIM** |
| 37 | Quectel EC20-CE (EC20F) | `EC20CEHCR06A03M1G` | 863418059992990 | `1-1.4/1-1.4.1` | ttyUSB10/11 | `/dev/cdc-wdm2` (QMI) | **空** |

## 各 SIM 详情

| SIM | 模块 | Kind | ICCID | IMSI | 运营商 | 号码 | 信号 | 注册状态 |
|---|---|---|---|---|---|---|---|---|
| 12 | 33 | pSIM | `8985203105011606981` | 454031051160698 | CHN-CT (45403) | - | 84% (LTE) | roaming |
| 13 | 35 | 5ber sticker eSIM | `8944110069156835483` | 234104846999661 | giffgaff (23410) | - | 62% (LTE, 漫游在 CMCC 46000) | roaming |
| 14 | 36 | 9eSIM sticker eSIM | - | 262036013169687 | 德国 262 MCC (漫游 CMCC 46000) | `+491791566795` | 67% (LTE) | roaming |

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
