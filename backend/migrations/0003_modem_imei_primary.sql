-- 0003_modem_imei_primary.sql
-- 同步副本，与 backend/internal/db/migrations/0003_modem_imei_primary.sql 保持一致。
-- 当前运行时仅 internal/db/migrations 生效（embed.FS），此文件为文档/手工参考。
--
-- 目标：把 modems 表的"稳定身份"从 device_id 改成 IMEI，修复"换 USB 口产生幽灵模块"。
--
-- 背景：
--   ModemManager 的 DeviceIdentifier (device_id) 依赖 USB sysfs path + 驱动信息，
--   更换 USB 口、模块重新 enumerate、或固件切身份（MBIM→cdc_ether）都会让它变。
--   结果同一物理模块（IMEI 不变）会产生新的 modems 行，老行 present=0 成为幽灵。
--
-- 方案：
--   1. 合并现有 DB 中按 IMEI 重复的 modems 行（保留最活跃的那条，把子表 FK 重指向它）
--   2. 重建 modems 表，去掉 device_id 的 UNIQUE 约束
--   3. 在 imei 上建 UNIQUE 索引（允许多个 IMEI 为 NULL/空串）
--
-- 注意：
--   * SQLite 不支持 DROP CONSTRAINT，只能 CREATE new + INSERT SELECT + DROP + RENAME。
--   * 子表（modem_sim_bindings / signal_samples / sms）通过 FK 引用 modems.id，
--     CASCADE / SET NULL 会在 DROP old 时触发破坏。migrator 已在应用本迁移前
--     PRAGMA foreign_keys=OFF，保证下面重建流程安全。
--   * INSERT SELECT 保留原 id，外部不用重指。

-- ---------- 第 1 步：合并 IMEI 重复行 ----------
-- 对每个非空 IMEI 分组，选一条"保留"行（present=1 优先，否则 last_seen_at 最近）；
-- 其余行的子表引用重指到保留行，然后删除其余行。

-- 1a. 构造 (keep_id, drop_id) 对到临时表
CREATE TEMP TABLE _modem_merge AS
WITH ranked AS (
    SELECT
        id,
        imei,
        ROW_NUMBER() OVER (
            PARTITION BY imei
            ORDER BY present DESC, last_seen_at DESC, id DESC
        ) AS rn
    FROM modems
    WHERE imei IS NOT NULL AND imei <> ''
),
keepers AS (
    SELECT imei, id AS keep_id FROM ranked WHERE rn = 1
)
SELECT
    k.keep_id AS keep_id,
    r.id      AS drop_id
FROM ranked r
JOIN keepers k ON k.imei = r.imei
WHERE r.rn > 1;

-- 1b. 把子表 modem_id 重指到 keep_id
UPDATE modem_sim_bindings
   SET modem_id = (SELECT keep_id FROM _modem_merge WHERE drop_id = modem_sim_bindings.modem_id)
 WHERE modem_id IN (SELECT drop_id FROM _modem_merge);

UPDATE signal_samples
   SET modem_id = (SELECT keep_id FROM _modem_merge WHERE drop_id = signal_samples.modem_id)
 WHERE modem_id IN (SELECT drop_id FROM _modem_merge);

UPDATE sms
   SET modem_id = (SELECT keep_id FROM _modem_merge WHERE drop_id = sms.modem_id)
 WHERE modem_id IN (SELECT drop_id FROM _modem_merge);

UPDATE ussd_sessions
   SET modem_id = (SELECT keep_id FROM _modem_merge WHERE drop_id = ussd_sessions.modem_id)
 WHERE modem_id IN (SELECT drop_id FROM _modem_merge);

-- 1c. modem_sim_bindings 的主键是 modem_id，上面重指可能触发冲突（同一 keeper 已有绑定）。
-- 去重：对每个 keeper 只保留 bound_at 最新的一条。
DELETE FROM modem_sim_bindings
 WHERE rowid NOT IN (
     SELECT MAX(rowid) FROM modem_sim_bindings GROUP BY modem_id
 );

-- 1d. 删除被合并的重复行
DELETE FROM modems WHERE id IN (SELECT drop_id FROM _modem_merge);

DROP TABLE _modem_merge;

-- ---------- 第 2 步：重建 modems 表去掉 device_id UNIQUE ----------
-- 新表：device_id NOT NULL（仍需存在用于 DBus 定位）但不再 UNIQUE。
CREATE TABLE modems_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id       TEXT NOT NULL,                  -- MM DeviceIdentifier；会随 USB 口/固件身份变化
    dbus_path       TEXT,
    manufacturer    TEXT,
    model           TEXT,
    firmware        TEXT,
    imei            TEXT,                           -- 稳定物理身份；UNIQUE 索引见下
    plugin          TEXT,
    primary_port    TEXT,
    at_ports        TEXT,
    qmi_port        TEXT,
    mbim_port       TEXT,
    usb_path        TEXT,
    present         INTEGER NOT NULL DEFAULT 1,
    first_seen_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    nickname        TEXT
);

INSERT INTO modems_new (
    id, device_id, dbus_path, manufacturer, model, firmware, imei, plugin,
    primary_port, at_ports, qmi_port, mbim_port, usb_path,
    present, first_seen_at, last_seen_at, nickname
)
SELECT
    id, device_id, dbus_path, manufacturer, model, firmware, imei, plugin,
    primary_port, at_ports, qmi_port, mbim_port, usb_path,
    present, first_seen_at, last_seen_at, nickname
FROM modems;

DROP TABLE modems;
ALTER TABLE modems_new RENAME TO modems;

-- ---------- 第 3 步：在 imei 上建 UNIQUE 索引 ----------
-- SQLite UNIQUE 把多个 NULL 视为不冲突，所以用完整 UNIQUE（非 partial）即可兼顾：
--   * 非空 IMEI：去重
--   * 空 IMEI：存为 NULL，多行可共存（极罕见：模块早期未识别 IMEI）
-- 注意：ON CONFLICT 目标不支持 partial index，这里必须用完整 UNIQUE 才能让 UpsertModem
-- 使用 `ON CONFLICT(imei)` 语法。
-- 为此先把现有空串 imei 统一规范化为 NULL。
UPDATE modems SET imei = NULL WHERE imei IS NOT NULL AND imei = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_modems_imei_unique ON modems(imei);

-- device_id 仍然需要能快速查询，也需要 UNIQUE 作为 IMEI 为空时 UpsertModem 的 ON CONFLICT 目标。
-- （device_id 是 MM 的 hash，实际上也不可能天然重复。）
CREATE UNIQUE INDEX IF NOT EXISTS idx_modems_device_id_unique ON modems(device_id);
