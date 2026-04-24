-- 0001_init.sql
-- ohmysmsapp 初始 schema（阶段 7 重置版；合并了旧 0001+0002+0003）。
--
-- 设计要点：
--   * modems：被 ModemManager 识别的物理 4G 模块。
--       稳定身份 = IMEI（UNIQUE，允许多个 NULL 行并存，覆盖"没识别出 IMEI"的罕见情况）。
--       次级身份 = device_id（UNIQUE，用作 IMEI 为空时的冲突键）。
--       换 USB 口 / 固件切身份导致 device_id 变化时，同 IMEI 会被 upsert 合并，
--       不再产生幽灵 present=0 的重复行。
--       含 nickname 字段，允许用户自定义备注。
--   * sims：当前或历史出现过的 SIM / eSIM profile，UNIQUE on ICCID。
--       只有拿到真实 ICCID 才入库（不再用 "imsi:<IMSI>" 合成 key）。
--   * signal_samples：含 SNR（dB，浮点）。
--   * sms：UNIQUE 键为 source_key（格式 "mm:<deviceID>:<DBus SMS path>"）；
--       这样同一 MM 对象在 provider 重启/重复 emit 时会被幂等合并，
--       而不依赖 (sim_id, ext_id)（NULL sim_id 不能被 UNIQUE 去重）。
--   * ussd_sessions：USSD 会话历史。
--   * audit_log：审计日志，阶段 7.4 用。
--   * 所有 TIMESTAMP 列改为 TEXT 并由应用层写入 RFC3339 UTC；
--       不再依赖 SQLite 的 CURRENT_TIMESTAMP（其格式为 "YYYY-MM-DD HH:MM:SS"，
--       缺 T/缺时区，前端 / 其它语言解析麻烦）。

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS modems (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id       TEXT NOT NULL,                  -- MM Modem.DeviceIdentifier（会随 USB 口/固件身份变化）
    dbus_path       TEXT,                           -- /org/freedesktop/ModemManager1/Modem/<n>（易变）
    manufacturer    TEXT,
    model           TEXT,
    firmware        TEXT,
    imei            TEXT,                           -- 稳定物理身份；UNIQUE 索引见下
    plugin          TEXT,                           -- quectel / huawei / ...
    primary_port    TEXT,                           -- cdc-wdm* / ttyUSB*
    at_ports        TEXT,                           -- JSON array
    qmi_port        TEXT,
    mbim_port       TEXT,
    usb_path        TEXT,                           -- sysfs USB path (1-1.4.2)
    present         INTEGER NOT NULL DEFAULT 1,
    nickname        TEXT,                           -- 用户自定义备注；NULL/空视为未设置
    first_seen_at   TEXT NOT NULL,                  -- RFC3339 UTC
    last_seen_at    TEXT NOT NULL                   -- RFC3339 UTC
);

-- IMEI 上建 UNIQUE 索引；SQLite 允许多个 NULL 共存。
-- 应用层写入时会把空串规范化为 NULL（见 store.go UpsertModem）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_modems_imei_unique ON modems(imei);
-- device_id 也需要 UNIQUE 用作 IMEI 为空时的 ON CONFLICT 目标。
CREATE UNIQUE INDEX IF NOT EXISTS idx_modems_device_id_unique ON modems(device_id);

CREATE TABLE IF NOT EXISTS esim_cards (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    eid             TEXT UNIQUE,                    -- eUICC EID，首次 lpac 探测成功后填充
    vendor          TEXT,                           -- fiveber / nineesim / unknown
    nickname        TEXT,                           -- 用户可编辑别名
    notes           TEXT,
    created_at      TEXT NOT NULL                   -- RFC3339 UTC
);

CREATE TABLE IF NOT EXISTS sims (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    iccid           TEXT NOT NULL UNIQUE,
    imsi            TEXT,
    msisdn          TEXT,                           -- 手机号（若可获取）
    operator_id     TEXT,                           -- MCC+MNC
    operator_name   TEXT,
    card_type       TEXT NOT NULL DEFAULT 'psim',   -- psim / sticker_esim / embedded_esim
    esim_card_id    INTEGER,                        -- 若属于某张 sticker eSIM
    esim_profile_active INTEGER NOT NULL DEFAULT 0, -- 该 profile 当前是否为 active
    esim_profile_nickname TEXT,
    first_seen_at   TEXT NOT NULL,                  -- RFC3339 UTC
    last_seen_at    TEXT NOT NULL,                  -- RFC3339 UTC
    FOREIGN KEY (esim_card_id) REFERENCES esim_cards(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_sims_esim ON sims(esim_card_id);

-- 当前 modem ↔ sim 绑定（只反映"现在是谁"，历史另记）
CREATE TABLE IF NOT EXISTS modem_sim_bindings (
    modem_id        INTEGER PRIMARY KEY,
    sim_id          INTEGER NOT NULL,
    bound_at        TEXT NOT NULL,                  -- RFC3339 UTC
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE CASCADE,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS signal_samples (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    modem_id        INTEGER NOT NULL,
    sim_id          INTEGER,
    quality_pct     INTEGER,                        -- 0-100（MM signal_quality）
    rssi_dbm        INTEGER,
    rsrp_dbm        INTEGER,
    rsrq_db         INTEGER,
    snr_db          REAL,                           -- LTE/NR 的 SNR（dB，浮点）
    access_tech     TEXT,                           -- lte / umts / gsm / 5gnr
    registration    TEXT,                           -- home / roaming / searching / denied
    operator_id     TEXT,
    operator_name   TEXT,
    sampled_at      TEXT NOT NULL,                  -- RFC3339 UTC
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE CASCADE,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_signal_modem_time ON signal_samples(modem_id, sampled_at DESC);

CREATE TABLE IF NOT EXISTS sms (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sim_id          INTEGER,
    modem_id        INTEGER,
    direction       TEXT NOT NULL,                  -- inbound / outbound
    state           TEXT NOT NULL,                  -- received / sending / sent / failed / stored
    peer            TEXT NOT NULL,                  -- 对端号码
    body            TEXT NOT NULL,
    pdu             BLOB,
    ext_id          TEXT,                           -- MM SMS DBus path 或服务商消息 ID（可为空；非唯一键）
    source_key      TEXT NOT NULL UNIQUE,           -- 去重键："mm:<deviceID>:<SMS path>"（MM 重启后 path 漂移可接受）
    ts_received     TEXT,                           -- 短信中心时间戳（RFC3339）
    ts_created      TEXT NOT NULL,                  -- RFC3339 UTC；应用层写
    ts_sent         TEXT,                           -- RFC3339
    error_message   TEXT,
    pushed_to_tg    INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE SET NULL,
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_sms_sim_time  ON sms(sim_id, ts_created DESC);
CREATE INDEX IF NOT EXISTS idx_sms_peer      ON sms(peer);
CREATE INDEX IF NOT EXISTS idx_sms_ext_id    ON sms(ext_id);

CREATE TABLE IF NOT EXISTS ussd_sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sim_id          INTEGER,
    modem_id        INTEGER,
    initial_request TEXT NOT NULL,
    state           TEXT NOT NULL,                  -- active / user_response / terminated / failed
    transcript      TEXT NOT NULL DEFAULT '[]',     -- JSON array of {dir,ts,text}
    started_at      TEXT NOT NULL,                  -- RFC3339 UTC
    ended_at        TEXT,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE SET NULL,
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL                        -- RFC3339 UTC
);

-- audit_log：记录所有写操作（HTTP / Telegram / 系统事件）。
-- payload 是 JSON，调用方做好脱敏（不存密码、token、完整短信正文等）。
CREATE TABLE IF NOT EXISTS audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    actor       TEXT NOT NULL,                      -- web:<username> / telegram:<chatID> / system
    action      TEXT NOT NULL,                      -- auth.login / sms.send / ussd.start / modem.reset / ...
    target      TEXT,                               -- 具体对象 id（sms id、device_id 等）
    payload     TEXT,                               -- JSON（已脱敏）
    result      TEXT NOT NULL,                      -- ok / error
    error       TEXT,
    created_at  TEXT NOT NULL                       -- RFC3339 UTC
);

CREATE INDEX IF NOT EXISTS idx_audit_time   ON audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor  ON audit_log(actor);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
