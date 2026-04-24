# ModemManager DBus 开发参考

> 来源：调研自 MM 1.24 官方 introspection XML + 实机 `busctl introspect` 验证  
> 本文件是阶段 2 实现的精确规格

[调研 + 规格详见此文件，内容来自 librarian 报告]

## A. 接口/方法/信号精确签名

### A.1 ObjectManager（根对象）

**Service:** `org.freedesktop.ModemManager1`  
**Object:** `/org/freedesktop/ModemManager1`  
**Interface:** `org.freedesktop.DBus.ObjectManager`

| 方法/信号 | DBus 签名 | 说明 |
|-----------|-----------|------|
| `GetManagedObjects()` | → `a{oa{sa{sv}}}` | 返回所有对象路径 → 接口名 → 属性字典 |
| Signal `InterfacesAdded` | `(o, a{sa{sv}})` | path + interfaces |
| Signal `InterfacesRemoved` | `(o, as)` | path + 被移除的接口名列表 |

### A.2 Modem 主接口（`org.freedesktop.ModemManager1.Modem`）

关键属性：
- `Manufacturer/Model/Revision/HardwareRevision` (s)
- `DeviceIdentifier` (s) — **稳定 hash，upsert key**
- `EquipmentIdentifier` (s) — IMEI
- `State` (**i**，有符号，FAILED=-1)
- `StateFailedReason` (u)
- `PowerState` (u)
- `AccessTechnologies` (u, bitmask)
- `SignalQuality` (ub) — struct{Value, Recent}
- `Ports` (a(su)) — [(name, type)]
- `PrimaryPort` (s)
- `Sim` (o) — SIM 对象路径，空为无卡
- `OwnNumbers` (as)
- `Physdev` (s, since 1.22) — sysfs 路径

信号：`StateChanged(iiu)` — old, new, reason

### A.3 Modem3gpp（`org.freedesktop.ModemManager1.Modem.Modem3gpp`）

- `Imei` (s)
- `OperatorCode` (s) — MCCMNC
- `OperatorName` (s)
- `RegistrationState` (u) — 见枚举

### A.4 Modem3gpp.Ussd（`...Modem3gpp.Ussd`）

方法：
- `Initiate(command)` → `s`（同步阻塞直到第一个网络回复）
- `Respond(response)` → `s`
- `Cancel()` → `()`

属性：
- `State` (u) — IDLE=1 / ACTIVE=2 / USER_RESPONSE=3
- `NetworkNotification` (s)
- `NetworkRequest` (s)

### A.5 Messaging（`org.freedesktop.ModemManager1.Modem.Messaging`）

方法：
- `List()` → `ao`
- `Create(a{sv})` → `o`（key: number/text/data/smsc/validity/class/delivery-report-request/storage）
- `Delete(o)` → `()`
- `SetDefaultStorage(u)` → `()`

信号：
- `Added(o, b)` — path, received（true = 来自网络）
- `Deleted(o)` — path

### A.6 Sms（`org.freedesktop.ModemManager1.Sms`）

方法：
- `Send()` — Create 后必须显式调用才真正发送
- `Store(u)` — 仅存储

关键属性：
- `State` (u) — UNKNOWN=0 / STORED=1 / RECEIVING=2 / RECEIVED=3 / SENDING=4 / SENT=5
- `Number` (s)
- `Text` (s) — 仅在 State=RECEIVED 时可安全读取
- `Data` (ay)
- `SMSC` (s)
- `Timestamp` (s, ISO8601)
- `DeliveryState` (u)
- `Storage` (u)

### A.7 Sim（`org.freedesktop.ModemManager1.Sim`）

属性：
- `SimIdentifier` (s) — ICCID
- `Imsi` (s)
- `Eid` (s, since 1.16) — eSIM EID
- `OperatorIdentifier` (s) — 注意是 Identifier 不是 Id
- `OperatorName` (s)
- `Active` (b, since 1.16)
- `EmergencyNumbers` (as, since 1.12)
- `SimType` (u, since 1.20)

### A.8 Signal（`org.freedesktop.ModemManager1.Modem.Signal`）

方法：
- `Setup(u)` — rate 秒，0=禁用
- `SetupThresholds(a{sv})` — since 1.20

属性（所有 dict 内部字段类型均为 `d` double）：
- `Lte` (a{sv}) — rssi/rsrq/rsrp/snr/error-rate
- `Nr5g` (a{sv}) — rsrq/rsrp/snr/error-rate（since 1.16，**无 rssi**）
- `Umts`/`Gsm`/`Cdma`/`Evdo` (a{sv})

---

## B. Go 代码片段

### 连接 System Bus + GetManagedObjects

```go
conn, _ := dbus.ConnectSystemBus()
defer conn.Close()

type ManagedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

obj := conn.Object("org.freedesktop.ModemManager1", "/org/freedesktop/ModemManager1")
result := make(ManagedObjects)
obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&result)

for path, ifaces := range result {
    if _, ok := ifaces["org.freedesktop.ModemManager1.Modem"]; ok {
        // ...
    }
}
```

### 订阅 InterfacesAdded/Removed

```go
conn.AddMatchSignal(
    dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
    dbus.WithMatchMember("InterfacesAdded"),
    dbus.WithMatchSender("org.freedesktop.ModemManager1"),
    dbus.WithMatchPathNamespace("/org/freedesktop/ModemManager1"),
)
ch := make(chan *dbus.Signal, 64)
conn.Signal(ch)

for sig := range ch {
    switch sig.Name {
    case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
        var path dbus.ObjectPath
        var ifaces map[string]map[string]dbus.Variant
        dbus.Store(sig.Body, &path, &ifaces)
    }
}
```

### State 属性解包（注意 int32 有符号）

```go
var state int32  // 不是 uint32！FAILED=-1
props["State"].Store(&state)
```

### SignalQuality 解包（DBus STRUCT → []interface{}）

```go
if s, ok := props["SignalQuality"].Value().([]interface{}); ok && len(s) == 2 {
    pct := s[0].(uint32)
    recent := s[1].(bool)
}
```

### Ports (a(su)) 解包

```go
if raw, ok := props["Ports"].Value().([][]interface{}); ok {
    for _, item := range raw {
        name := item[0].(string)
        typ := item[1].(uint32)
    }
}
```

### 发短信

```go
smsProps := map[string]dbus.Variant{
    "number": dbus.MakeVariant(to),
    "text":   dbus.MakeVariant(text),
}
var smsPath dbus.ObjectPath
modemObj.Call("org.freedesktop.ModemManager1.Modem.Messaging.Create", 0, smsProps).Store(&smsPath)

smsObj := conn.Object("org.freedesktop.ModemManager1", smsPath)
smsObj.Call("org.freedesktop.ModemManager1.Sms.Send", 0)
```

### 订阅 Messaging.Added

```go
conn.AddMatchSignal(
    dbus.WithMatchInterface("org.freedesktop.ModemManager1.Modem.Messaging"),
    dbus.WithMatchMember("Added"),
    dbus.WithMatchObjectPath(modemPath),
    dbus.WithMatchSender("org.freedesktop.ModemManager1"),
)
// 收到后：
var smsPath dbus.ObjectPath
var received bool
dbus.Store(sig.Body, &smsPath, &received)
// 再检查 State=3 (RECEIVED) 才读 Text
```

### USSD Initiate

```go
ussdObj := conn.Object("org.freedesktop.ModemManager1", modemPath)
var reply string
ussdObj.Call("org.freedesktop.ModemManager1.Modem.Modem3gpp.Ussd.Initiate", 0, code).Store(&reply)
// Initiate 已经阻塞并拿到第一个回复
// 若后续需要多轮：监听 PropertiesChanged，State=3 (USER_RESPONSE) 时读 NetworkRequest 再 Respond
```

---

## C. 枚举值表（摘要）

### MMModemState (i)
- -1 FAILED · 0 UNKNOWN · 1 INITIALIZING · 2 LOCKED · 3 DISABLED · 4 DISABLING
- 5 ENABLING · 6 ENABLED · 7 SEARCHING · 8 REGISTERED · 9 DISCONNECTING · 10 CONNECTING · 11 CONNECTED

### MMModemStateFailedReason (u)
- 0 NONE · 1 UNKNOWN · 2 SIM_MISSING · 3 SIM_ERROR
- 4 UNKNOWN_CAPABILITIES (1.20) · 5 ESIM_WITHOUT_PROFILES (1.20)

### MMModem3gppRegistrationState (u)
- 0 IDLE · 1 HOME · 2 SEARCHING · 3 DENIED · 4 UNKNOWN · 5 ROAMING
- 6 HOME_SMS_ONLY · 7 ROAMING_SMS_ONLY · 8 EMERGENCY_ONLY
- 9 HOME_CSFB_NOT_PREFERRED · 10 ROAMING_CSFB_NOT_PREFERRED · 11 ATTACHED_RLOS

### MMSmsState (u)
- 0 UNKNOWN · 1 STORED · 2 RECEIVING · 3 RECEIVED · 4 SENDING · 5 SENT

### MMSmsStorage (u)
- 0 UNKNOWN · 1 SM · 2 ME · 3 MT · 4 SR · 5 BM · 6 TA

### MMModem3gppUssdSessionState (u)
- 0 UNKNOWN · 1 IDLE · 2 ACTIVE · 3 USER_RESPONSE

### MMModemPortType (u)
- 1 UNKNOWN · 2 NET · 3 AT · 4 QCDM · 5 GPS · 6 QMI · 7 MBIM · 8 AUDIO · 9 IGNORED · 10 XMMRPC (1.24)

### MMModemAccessTechnology (u, bitmask)
- 0x0002 GSM · 0x0020 UMTS · 0x4000 LTE · 0x8000 5GNR
- 0x10000 LTE_CAT_M · 0x20000 LTE_NB_IOT
- 0xFFFFFFFF ANY

---

## D. 实机差异注意

### Huawei ME906s（Modem 12，cdc_mbim）
- **无 AT 端口**：`Ports` 不含 AT(3)
- **USSD 接口可能缺失**：运行时用 `GetManagedObjects` 返回的接口列表判断
- **SMS 仅支持 ME 存储**：`SupportedStorages = [ME]`
- **Signal 接口**：部分固件不上报或 Setup 无效
- **OwnNumbers 可读**：`+491791566795`

### Quectel EC20-CE（Modem 10/11/13）
- 有多个 ttyUSB + cdc-wdm
- SMS 支持 SM/ME 两种 storage
- Signal 接口完整支持 LTE
- USSD 支持

### 实时判断方法

```go
managed, _ := listManagedObjects(conn)
ifaces := managed[modemPath]
_, hasUssd := ifaces["org.freedesktop.ModemManager1.Modem.Modem3gpp.Ussd"]
_, hasSignal := ifaces["org.freedesktop.ModemManager1.Modem.Signal"]
_, hasMessaging := ifaces["org.freedesktop.ModemManager1.Modem.Messaging"]
```

---

## E. 陷阱

1. **State 是 int32**（FAILED=-1），用 uint32 会变 4294967295
2. **SMS 多段**：Added 信号触发时 State 可能是 RECEIVING，需监听 PropertiesChanged 等 RECEIVED
3. **Create 后必须显式 Send**，对象默认 STORED 状态
4. **DBus path 易变**：MM 重启会变，用 DeviceIdentifier upsert
5. **SIM path 也可能变**：modem 从 failed 恢复时要重新读 `Sim` 属性
6. **Signal 空 dict**：某技术未激活时字典为空（非 nil），读取前检查 key

---

## F. 权限

生产部署：systemd `User=root`（当前方案）。  
未来降权：polkit 规则允许 `dialout` 组调 `org.freedesktop.ModemManager1.*`。

---

## G. 参考

- [MM introspection XML (main)](https://gitlab.freedesktop.org/mobile-broadband/ModemManager/-/tree/main/introspection)
- [MM enums.h](https://gitlab.freedesktop.org/mobile-broadband/ModemManager/-/raw/main/include/ModemManager-enums.h)
- [godbus/dbus v5](https://pkg.go.dev/github.com/godbus/dbus/v5)
