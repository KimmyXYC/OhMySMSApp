package modem

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Store 封装 ohmysmsapp 对 modem/sim/sms/ussd 相关表的所有写入与查询。
//
// 设计：所有操作都接受 ctx；内部用 SQLite upsert 以避免并发竞态。
// 返回的 int64 是对应行的主键 id，便于后续关联写入（例如 sms.modem_id）。
//
// 时间戳约定：所有写入的时间列都由应用层产生 RFC3339 UTC 字符串，
// schema 层已去掉 CURRENT_TIMESTAMP 默认值——这样前端/其它语言解析更省心。
type Store struct {
	db *sql.DB
}

// NewStore 构造 Store。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// nowRFC3339 返回当前 UTC 时间的 RFC3339 字符串。
// 统一使用此 helper，避免每处都写 time.Now().UTC().Format(...)。
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// sourceKeyForSMS 构造 sms.source_key 的统一格式。
// deviceID 比 modem 主键更稳定（modem id 是本地自增，跨重装 DB 后会变；
// deviceID 是 MM 分配的 hash，即使换 USB 口也能配合 IMEI 合并）。
// ext_id 通常是 MM SMS DBus path；MM 重启后 path 会重分配，这意味着
// 跨 MM 重启同一条短信会产生不同 source_key → 重复插入一次。可接受。
func sourceKeyForSMS(deviceID, extID string) string {
	return fmt.Sprintf("mm:%s:%s", deviceID, extID)
}

// UpsertModem 按"稳定身份"upsert 一条 modem 记录，并返回其自增主键。
//
// 稳定身份优先级：
//  1. 若 state.IMEI != ""：以 imei 作为冲突键。这样同一物理模块换 USB 口、
//     或固件切换身份（例如 Huawei ME906s 从 ME906s 变成 ME909s-821）
//     导致 MM DeviceIdentifier 变化时，新 device_id 会覆盖旧 device_id 到同一行，
//     不再产生幽灵 present=0 行。
//  2. 若 IMEI 为空（极罕见：模块早期未识别 IMEI）：回退到 device_id 冲突键，
//     避免破坏原有行为；这类行通常后续读到 IMEI 后会被合并或独立存在。
func (s *Store) UpsertModem(ctx context.Context, m ModemState) (int64, error) {
	atPortsJSON, _ := json.Marshal(collectPortNames(m.Ports, "at"))
	qmiPort := firstPortByType(m.Ports, "qmi")
	mbimPort := firstPortByType(m.Ports, "mbim")

	var conflictKey string
	if m.IMEI != "" {
		conflictKey = "imei"
	} else {
		conflictKey = "device_id"
	}

	// imei 字段存储规范：空串统一写 NULL。
	// SQLite UNIQUE 视多个 NULL 为不冲突，这样：
	//  - 同一 IMEI 的行被 idx_modems_imei_unique 合并（修复幽灵）
	//  - IMEI 缺失的罕见场景多条能并存，不会被错误去重
	var imeiArg any
	if m.IMEI == "" {
		imeiArg = nil
	} else {
		imeiArg = m.IMEI
	}

	now := nowRFC3339()

	q := `
INSERT INTO modems(
    device_id, dbus_path, manufacturer, model, firmware, imei, plugin,
    primary_port, at_ports, qmi_port, mbim_port, usb_path,
    present, first_seen_at, last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
ON CONFLICT(` + conflictKey + `) DO UPDATE SET
    device_id    = excluded.device_id,
    dbus_path    = excluded.dbus_path,
    manufacturer = excluded.manufacturer,
    model        = excluded.model,
    firmware     = excluded.firmware,
    imei         = excluded.imei,
    plugin       = excluded.plugin,
    primary_port = excluded.primary_port,
    at_ports     = excluded.at_ports,
    qmi_port     = excluded.qmi_port,
    mbim_port    = excluded.mbim_port,
    usb_path     = excluded.usb_path,
    present      = 1,
    last_seen_at = excluded.last_seen_at
`
	if _, err := s.db.ExecContext(ctx, q,
		m.DeviceID, m.DBusPath, m.Manufacturer, m.Model, m.Revision, imeiArg, m.Plugin,
		m.PrimaryPort, string(atPortsJSON), qmiPort, mbimPort, m.USBPath,
		now, now,
	); err != nil {
		return 0, fmt.Errorf("upsert modem: %w", err)
	}
	// 用 device_id 反查 id（upsert 后此刻该 device_id 已是最新的）。
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM modems WHERE device_id = ?`, m.DeviceID,
	).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// MarkModemAbsent 在 modem 从 DBus 消失时调用，present 置 0。device_id 行本身保留。
func (s *Store) MarkModemAbsent(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE modems SET present = 0, last_seen_at = ? WHERE device_id = ?`,
		nowRFC3339(), deviceID)
	return err
}

// MarkStaleModemsAbsent 把 DB 里 present=1 但 device_id 不在 seenDeviceIDs 中的
// modem 行标记为 present=0，并清空其 SIM 绑定。返回受影响行数。
//
// 用途：服务启动时可能残留旧 device_id 的幽灵记录（比如上次运行时 present=1，
// 用户关机换了 USB 口再开机，新 device_id 被 upsert 进来，但老 device_id 的那条
// 仍是 present=1）。
//
// 特殊情况：seenDeviceIDs 为空 → 不做任何事，避免在 provider 尚未识别到任何模块
// 时把全库模块都清成 absent。
func (s *Store) MarkStaleModemsAbsent(ctx context.Context, seenDeviceIDs []string) (int64, error) {
	if len(seenDeviceIDs) == 0 {
		return 0, nil
	}

	// 构造 IN (?,?,...) 占位符
	placeholders := make([]string, len(seenDeviceIDs))
	args := make([]any, len(seenDeviceIDs))
	for i, d := range seenDeviceIDs {
		placeholders[i] = "?"
		args[i] = d
	}
	inList := strings.Join(placeholders, ",")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	// 先清这些即将被标记 absent 的行的 sim 绑定，避免前端残留
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM modem_sim_bindings WHERE modem_id IN (
            SELECT id FROM modems WHERE present = 1 AND device_id NOT IN (`+inList+`)
        )`, args...); err != nil {
		return 0, fmt.Errorf("unbind stale modems: %w", err)
	}

	// 更新时追加一个额外的时间戳参数
	updateArgs := append([]any{nowRFC3339()}, args...)
	res, err := tx.ExecContext(ctx,
		`UPDATE modems SET present = 0, last_seen_at = ?
         WHERE present = 1 AND device_id NOT IN (`+inList+`)`, updateArgs...)
	if err != nil {
		return 0, fmt.Errorf("mark stale modems absent: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// SetModemNickname 设置 modem 的用户备注。nickname 为空串时写入 NULL（清空）。
// 若 device_id 不存在，返回 sql.ErrNoRows。
func (s *Store) SetModemNickname(ctx context.Context, deviceID, nickname string) error {
	nickname = strings.TrimSpace(nickname)
	var arg any
	if nickname == "" {
		arg = nil
	} else {
		arg = nickname
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE modems SET nickname = ? WHERE device_id = ?`, arg, deviceID)
	if err != nil {
		return fmt.Errorf("set modem nickname: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UnbindModem 删除 modem 的 modem_sim_bindings 行。
// 当 SIM 被拔出（HasSim=false）或 modem 下线时调用，避免前端继续看到历史 SIM 绑定。
// SIM 自身不删（保留短信/信号历史），只断开当前活跃关系。
func (s *Store) UnbindModem(ctx context.Context, modemID int64) error {
	if modemID <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM modem_sim_bindings WHERE modem_id = ?`, modemID)
	return err
}

// UpsertSim upsert SIM 行，并同步更新 modem ↔ sim 绑定。返回 sim.id。
//
// 只有存在真实 ICCID 才入库。空 ICCID 直接返回 (0, nil) —— 不再用
// "imsi:<IMSI>" 合成 fallback，避免把同一张卡在不同时间点 ICCID 可读/不可读
// 分裂成两行历史。
func (s *Store) UpsertSim(ctx context.Context, sim SimState, modemID int64) (int64, error) {
	if sim.ICCID == "" {
		return 0, nil
	}
	// 防御：万一调用方没规范化，这里再处理一次。把所有写入归一为无 padding 形式，
	// 否则会和 esim_profiles.iccid（lpac 输出已是无 padding）对不上。
	iccid := NormalizeICCID(sim.ICCID)
	if iccid == "" {
		return 0, nil
	}

	cardType := "psim"
	if strings.EqualFold(sim.SimType, "esim") {
		cardType = "sticker_esim"
	}

	now := nowRFC3339()

	const q = `
INSERT INTO sims(
    iccid, imsi, msisdn, operator_id, operator_name, card_type,
    first_seen_at, last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(iccid) DO UPDATE SET
    imsi          = CASE WHEN excluded.imsi <> '' THEN excluded.imsi ELSE sims.imsi END,
    msisdn        = CASE WHEN excluded.msisdn IS NOT NULL AND excluded.msisdn <> '' THEN excluded.msisdn ELSE sims.msisdn END,
    operator_id   = CASE WHEN excluded.operator_id <> '' THEN excluded.operator_id ELSE sims.operator_id END,
    operator_name = CASE WHEN excluded.operator_name <> '' THEN excluded.operator_name ELSE sims.operator_name END,
    -- 只把现存的 'psim' 升级为 excluded 提供的更具体类型；不要把 esim 服务已经识别出来的
    -- 'sticker_esim'/'embedded_esim' 覆盖回去（modem provider 拿到的 SimState.SimType 在
    -- 大多数模块上都是 unknown，会被解码成 psim，不能信任）。
    card_type     = CASE
        WHEN sims.card_type IN ('sticker_esim', 'embedded_esim') THEN sims.card_type
        ELSE excluded.card_type
    END,
    last_seen_at  = excluded.last_seen_at
`
	var msisdnArg any
	if sim.MSISDN != "" {
		msisdnArg = sim.MSISDN
	} else {
		msisdnArg = nil
	}
	if _, err := s.db.ExecContext(ctx, q,
		iccid, sim.IMSI, msisdnArg, sim.OperatorID, sim.OperatorName, cardType,
		now, now,
	); err != nil {
		return 0, fmt.Errorf("upsert sim: %w", err)
	}
	var simID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM sims WHERE iccid = ?`, iccid,
	).Scan(&simID); err != nil {
		return 0, err
	}

	if modemID > 0 {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO modem_sim_bindings(modem_id, sim_id, bound_at)
VALUES (?, ?, ?)
ON CONFLICT(modem_id) DO UPDATE SET
    sim_id   = excluded.sim_id,
    bound_at = excluded.bound_at
`, modemID, simID, now); err != nil {
			return simID, fmt.Errorf("bind modem sim: %w", err)
		}
	}
	return simID, nil
}

// InsertSMS 将一条 SMS 写入 db。去重键为 source_key（"mm:<deviceID>:<ext_id>"）。
// 冲突时更新 state / body（若新 body 非空）/ ts_received / ts_sent / error_message。
// sim_id / modem_id 可以为 0（NULL）。
func (s *Store) InsertSMS(ctx context.Context, rec SMSRecord, deviceID string, modemID, simID int64) error {
	if rec.ExtID == "" {
		return errors.New("sms record missing ext_id")
	}
	if deviceID == "" {
		return errors.New("sms insert requires deviceID for source_key")
	}
	sourceKey := sourceKeyForSMS(deviceID, rec.ExtID)

	var simArg any
	if simID > 0 {
		simArg = simID
	} else {
		simArg = nil
	}
	var modemArg any
	if modemID > 0 {
		modemArg = modemID
	} else {
		modemArg = nil
	}
	var tsRecv any
	if !rec.Timestamp.IsZero() {
		tsRecv = rec.Timestamp.UTC().Format(time.RFC3339)
	}
	var tsSent any
	if rec.State == "sent" {
		tsSent = nowRFC3339()
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO sms(sim_id, modem_id, direction, state, peer, body, ext_id, source_key,
                ts_received, ts_created, ts_sent)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_key) DO UPDATE SET
    state       = excluded.state,
    body        = CASE WHEN excluded.body <> '' THEN excluded.body ELSE sms.body END,
    ts_received = COALESCE(excluded.ts_received, sms.ts_received),
    ts_sent     = COALESCE(excluded.ts_sent, sms.ts_sent)
`,
		simArg, modemArg, rec.Direction, rec.State, rec.Peer, rec.Text,
		rec.ExtID, sourceKey, tsRecv, nowRFC3339(), tsSent,
	)
	if err != nil {
		return fmt.Errorf("insert sms: %w", err)
	}
	return nil
}

// UpdateSMSState 按 source_key 更新 state（以及可选的 error_message）。
// 调用方提供 deviceID + extID（runner 有这俩上下文）。
func (s *Store) UpdateSMSState(ctx context.Context, deviceID, extID, state, errMsg string) error {
	if deviceID == "" || extID == "" {
		return nil
	}
	sourceKey := sourceKeyForSMS(deviceID, extID)
	var errArg any
	if errMsg != "" {
		errArg = errMsg
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sms SET state = ?, error_message = COALESCE(?, error_message) WHERE source_key = ?`,
		state, errArg, sourceKey)
	return err
}

// InsertSignalSample 保存一条信号采样，供图表展示。
func (s *Store) InsertSignalSample(ctx context.Context, modemID, simID int64, sample SignalSample) error {
	var simArg any
	if simID > 0 {
		simArg = simID
	} else {
		simArg = nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO signal_samples(
    modem_id, sim_id, quality_pct, rssi_dbm, rsrp_dbm, rsrq_db, snr_db,
    access_tech, registration, operator_id, operator_name, sampled_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		modemID, simArg, sample.QualityPct,
		nullableInt(sample.RSSIdBm), nullableInt(sample.RSRPdBm), nullableInt(sample.RSRQdB),
		nullableFloat(sample.SNRdB),
		sample.AccessTech, sample.Registration, sample.OperatorID, sample.OperatorName,
		sample.SampledAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert signal sample: %w", err)
	}
	return nil
}

// ModemIDByDevice 查询 modem.id；不存在返回 sql.ErrNoRows。
func (s *Store) ModemIDByDevice(ctx context.Context, deviceID string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM modems WHERE device_id = ?`, deviceID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return id, err
}

// SimIDByModem 查绑定在某 modem 上的当前 sim_id；未绑定返回 0 且 err=nil。
func (s *Store) SimIDByModem(ctx context.Context, modemID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT sim_id FROM modem_sim_bindings WHERE modem_id = ?`, modemID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// AppendUSSD 向已有会话的 transcript 追加一行；会话不存在时创建。
// dir ∈ {"out","in","notification"}。
func (s *Store) AppendUSSD(ctx context.Context, sessionID, dir, text string, modemID int64) error {
	// sessionID 约定 = DeviceID（MM 无显式 id），我们以 (modem_id, 最后一条 active 行) 追加。
	// 简化：若已存在活跃会话（state ∈ active/user_response），就追加；否则新建。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var id int64
	var transcript string
	err = tx.QueryRowContext(ctx, `
SELECT id, transcript FROM ussd_sessions
WHERE modem_id = ? AND state IN ('active','user_response')
ORDER BY id DESC LIMIT 1`, modemID).Scan(&id, &transcript)
	now := nowRFC3339()
	entry := map[string]string{"dir": dir, "ts": now, "text": text}

	if errors.Is(err, sql.ErrNoRows) {
		arr, _ := json.Marshal([]map[string]string{entry})
		_, err = tx.ExecContext(ctx, `
INSERT INTO ussd_sessions(modem_id, initial_request, state, transcript, started_at)
VALUES (?, ?, 'active', ?, ?)`, modemID, text, string(arr), now)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		var arr []map[string]string
		_ = json.Unmarshal([]byte(transcript), &arr)
		arr = append(arr, entry)
		raw, _ := json.Marshal(arr)
		if _, err := tx.ExecContext(ctx,
			`UPDATE ussd_sessions SET transcript = ? WHERE id = ?`, string(raw), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetUSSDState 更新最近一条活跃会话的 state（terminated/failed/user_response/active）。
func (s *Store) SetUSSDState(ctx context.Context, modemID int64, state string) error {
	finished := state == "terminated" || state == "failed" || state == "idle"
	if finished {
		_, err := s.db.ExecContext(ctx, `
UPDATE ussd_sessions SET state = ?, ended_at = ?
WHERE modem_id = ? AND state IN ('active','user_response')`, state, nowRFC3339(), modemID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE ussd_sessions SET state = ?
WHERE modem_id = ? AND state IN ('active','user_response')`, state, modemID)
	return err
}

// ------------------- 内部 helper -------------------

func collectPortNames(ports []Port, typ string) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		if strings.EqualFold(p.Type, typ) {
			out = append(out, p.Name)
		}
	}
	return out
}

func firstPortByType(ports []Port, typ string) string {
	for _, p := range ports {
		if strings.EqualFold(p.Type, typ) {
			return p.Name
		}
	}
	return ""
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
