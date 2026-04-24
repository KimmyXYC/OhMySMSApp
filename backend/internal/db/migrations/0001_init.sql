-- 0001_init.sql
-- ohmysmsapp 初始 schema
-- 设计原则：
--  * modems：被 ModemManager 识别的物理 4G 模块，通过 device_id (MM 提供的稳定 hash) 去重
--  * sims：当前或历史出现过的 SIM/profile；对 sticker eSIM，同一 ICCID 会持久化
--  * esim_cards：sticker eSIM 本体（一张物理卡可能承载多个 profile，profile=sims 行）
--  * sms：短信；同一条可能被多次重复收到，用 (sim_id, ext_id) 去重
--  * ussd_sessions：USSD 会话历史，含请求/响应和结束状态

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS modems (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id       TEXT NOT NULL UNIQUE,          -- MM 的 Modem.DeviceIdentifier
    dbus_path       TEXT,                           -- /org/freedesktop/ModemManager1/Modem/<n>（易变）
    manufacturer    TEXT,
    model           TEXT,
    firmware        TEXT,
    imei            TEXT,
    plugin          TEXT,                           -- quectel / huawei / ...
    primary_port    TEXT,                           -- cdc-wdm* / ttyUSB*
    at_ports        TEXT,                           -- JSON array
    qmi_port        TEXT,                           -- /dev/cdc-wdm?
    mbim_port       TEXT,                           -- /dev/cdc-wdm?
    usb_path        TEXT,                           -- sysfs USB path (1-1.4.2)
    present         INTEGER NOT NULL DEFAULT 1,     -- 当前是否可见
    first_seen_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS esim_cards (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    eid             TEXT UNIQUE,                   -- eUICC EID，首次 lpac 探测成功后填充
    vendor          TEXT,                           -- fiveber / nineesim / unknown
    nickname        TEXT,                           -- 用户可编辑别名
    notes           TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    first_seen_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (esim_card_id) REFERENCES esim_cards(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_sims_esim ON sims(esim_card_id);

-- 当前 modem ↔ sim 绑定（只反映"现在是谁"，历史另记）
CREATE TABLE IF NOT EXISTS modem_sim_bindings (
    modem_id        INTEGER PRIMARY KEY,
    sim_id          INTEGER NOT NULL,
    bound_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
    access_tech     TEXT,                           -- lte / umts / gsm / 5gnr
    registration    TEXT,                           -- home / roaming / searching / denied
    operator_id     TEXT,
    operator_name   TEXT,
    sampled_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
    ext_id          TEXT,                           -- MM SMS DBus path 或服务商消息 ID
    ts_received     DATETIME,                       -- 短信中心时间戳
    ts_created      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ts_sent         DATETIME,
    error_message   TEXT,
    pushed_to_tg    INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE SET NULL,
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE SET NULL,
    UNIQUE (sim_id, ext_id)
);

CREATE INDEX IF NOT EXISTS idx_sms_sim_time  ON sms(sim_id, ts_created DESC);
CREATE INDEX IF NOT EXISTS idx_sms_peer      ON sms(peer);

CREATE TABLE IF NOT EXISTS ussd_sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sim_id          INTEGER,
    modem_id        INTEGER,
    initial_request TEXT NOT NULL,
    state           TEXT NOT NULL,                  -- active / user_response / terminated / failed
    transcript      TEXT NOT NULL DEFAULT '[]',     -- JSON array of {dir,ts,text}
    started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at        DATETIME,
    FOREIGN KEY (sim_id)   REFERENCES sims(id)   ON DELETE SET NULL,
    FOREIGN KEY (modem_id) REFERENCES modems(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key     TEXT PRIMARY KEY,
    value   TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    actor       TEXT NOT NULL,                      -- web / telegram / system
    action      TEXT NOT NULL,                      -- sms.send / ussd.start / esim.enable / ...
    target      TEXT,
    payload     TEXT,                               -- JSON
    result      TEXT NOT NULL,                      -- ok / error
    error       TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_log(created_at DESC);
