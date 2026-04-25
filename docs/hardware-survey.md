# 硬件勘察报告（已脱敏）

> 日期：2026-04-24  
> 目标设备：一台 Linux ARM64 主机（示例）  
> OS：Debian 13 / Linux 6.x / aarch64  
> ModemManager 1.24.x / NetworkManager 1.52.x

本文档保留对项目设计有帮助的技术结论，移除了真实 IP、账号、IMEI、ICCID、IMSI、手机号等信息。

## 模块与 SIM 概况

- 存在多块 Qualcomm/Quectel 4G 模块
- 存在一块 Huawei MBIM 模块
- 存在 pSIM、5ber sticker eSIM、9eSIM sticker eSIM 场景

## 关键结论

### 1. ModemManager 标识稳定性

- `/org/freedesktop/ModemManager1/Modem/<N>` 会变
- `cdc-wdm<N>` / `ttyUSB<N>` 也可能因枚举顺序变化
- 更稳定的标识通常是：
  - `Modem.DeviceIdentifier`
  - IMEI（如果可获取且稳定）

### 2. eSIM 切换能力探测

#### Quectel / Qualcomm 模块

- 标准 LPA APDU 通过 AT 指令通常不可行
- 更适合走 **QMI UIM** 路径
- lpac 在 QMI transport 下可工作

#### Huawei MBIM 模块

- 主要走 **MBIM** 路径
- 部分固件对 ModemManager 的 `Reset()` 支持较差，可能返回 unsupported

### 3. eSIM 管理策略

项目采用：

- `lpac` 作为独立进程管理 eSIM profile
- 操作前后通过 `ModemManager.InhibitDevice()` 让 MM 放手/重新接管
- sticker eSIM 按 transport 区分（QMI / MBIM），而非只按卡商区分

### 4. 设计约束

1. 模块能力分级：不同模块对短信、USSD、信号、eSIM 管理支持程度不同
2. eSIM 切换后，eUICC 状态与 modem 当前识别到的 SIM 状态可能存在短暂不一致
3. 某些模块/固件需要额外的 modem reset 或重新接管步骤，才能让新 profile 真正生效

## 参考

- lpac：<https://github.com/estkme-group/lpac>
- lpac USAGE：<https://github.com/estkme-group/lpac/blob/main/docs/USAGE.md>
- EasyLPAC：<https://github.com/creamlike1024/EasyLPAC>
- ModemManager D-Bus API：<https://www.freedesktop.org/software/ModemManager/doc/latest/>
