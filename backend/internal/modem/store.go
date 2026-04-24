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
type Store struct {
	db *sql.DB
}

// NewStore 构造 Store。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// UpsertModem 按 device_id upsert 一条 modem 记录，并返回其自增主键。
func (s *Store) UpsertModem(ctx context.Context, m ModemState) (int64, error) {
	atPortsJSON, _ := json.Marshal(collectPortNames(m.Ports, "at"))
	qmiPort := firstPortByType(m.Ports, "qmi")
	mbimPort := firstPortByType(m.Ports, "mbim")

	// present=1, last_seen_at=now；first_seen_at 由 INSERT 默认值提供。
	const q = `
INSERT INTO modems(
    device_id, dbus_path, manufacturer, model, firmware, imei, plugin,
    primary_port, at_ports, qmi_port, mbim_port, usb_path, present, last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
ON CONFLICT(device_id) DO UPDATE SET
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
    last_seen_at = CURRENT_TIMESTAMP
`
	if _, err := s.db.ExecContext(ctx, q,
		m.DeviceID, m.DBusPath, m.Manufacturer, m.Model, m.Revision, m.IMEI, m.Plugin,
		m.PrimaryPort, string(atPortsJSON), qmiPort, mbimPort, m.USBPath,
	); err != nil {
		return 0, fmt.Errorf("upsert modem: %w", err)
	}
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
		`UPDATE modems SET present = 0, last_seen_at = CURRENT_TIMESTAMP WHERE device_id = ?`,
		deviceID)
	return err
}

// UpsertSim upsert SIM 行，并同步更新 modem ↔ sim 绑定。返回 sim.id。
//
// 当 sim.ICCID 为空但 IMSI 不为空时，使用 "imsi:<IMSI>" 作为合成标识保证可持久化
// （某些 MBIM 固件如 Huawei ME906s 不上报 ICCID，但 IMSI 可读）。两者均为空时返回 0。
func (s *Store) UpsertSim(ctx context.Context, sim SimState, modemID int64) (int64, error) {
	iccid := sim.ICCID
	if iccid == "" {
		if sim.IMSI == "" {
			return 0, nil
		}
		iccid = "imsi:" + sim.IMSI
	}

	cardType := "psim"
	if strings.EqualFold(sim.SimType, "esim") {
		cardType = "sticker_esim"
	}

	const q = `
INSERT INTO sims(
    iccid, imsi, operator_id, operator_name, card_type, last_seen_at
) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(iccid) DO UPDATE SET
    imsi          = CASE WHEN excluded.imsi <> '' THEN excluded.imsi ELSE sims.imsi END,
    operator_id   = CASE WHEN excluded.operator_id <> '' THEN excluded.operator_id ELSE sims.operator_id END,
    operator_name = CASE WHEN excluded.operator_name <> '' THEN excluded.operator_name ELSE sims.operator_name END,
    card_type     = excluded.card_type,
    last_seen_at  = CURRENT_TIMESTAMP
`
	if _, err := s.db.ExecContext(ctx, q,
		iccid, sim.IMSI, sim.OperatorID, sim.OperatorName, cardType,
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
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(modem_id) DO UPDATE SET
    sim_id   = excluded.sim_id,
    bound_at = CURRENT_TIMESTAMP
`, modemID, simID); err != nil {
			return simID, fmt.Errorf("bind modem sim: %w", err)
		}
	}
	return simID, nil
}

// InsertSMS 将一条 SMS 写入 db。冲突键为 (sim_id, ext_id)，冲突时转为更新 state。
// sim_id 可以为 0（NULL）。
func (s *Store) InsertSMS(ctx context.Context, rec SMSRecord, modemID, simID int64) error {
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
		tsSent = time.Now().UTC().Format(time.RFC3339)
	}

	// 使用 INSERT OR IGNORE 再做一次 UPDATE，更稳健地处理冲突 & NULL sim_id 的 UNIQUE 行为差异。
	res, err := s.db.ExecContext(ctx, `
INSERT INTO sms(sim_id, modem_id, direction, state, peer, body, ext_id, ts_received, ts_sent)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(sim_id, ext_id) DO UPDATE SET
    state       = excluded.state,
    body        = CASE WHEN excluded.body <> '' THEN excluded.body ELSE sms.body END,
    ts_received = COALESCE(excluded.ts_received, sms.ts_received),
    ts_sent     = COALESCE(excluded.ts_sent, sms.ts_sent)
`,
		simArg, modemArg, rec.Direction, rec.State, rec.Peer, rec.Text, rec.ExtID, tsRecv, tsSent,
	)
	if err != nil {
		return fmt.Errorf("insert sms: %w", err)
	}
	_, _ = res.RowsAffected()
	return nil
}

// UpdateSMSState 按 ext_id 更新状态；若 ext_id 匹配多行（不同 sim_id）则一起更新。
func (s *Store) UpdateSMSState(ctx context.Context, extID, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sms SET state = ? WHERE ext_id = ?`, state, extID)
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
    modem_id, sim_id, quality_pct, rssi_dbm, rsrp_dbm, rsrq_db,
    access_tech, registration, operator_id, operator_name, sampled_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		modemID, simArg, sample.QualityPct,
		nullableInt(sample.RSSIdBm), nullableInt(sample.RSRPdBm), nullableInt(sample.RSRQdB),
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
	now := time.Now().UTC().Format(time.RFC3339)
	entry := map[string]string{"dir": dir, "ts": now, "text": text}

	if errors.Is(err, sql.ErrNoRows) {
		arr, _ := json.Marshal([]map[string]string{entry})
		_, err = tx.ExecContext(ctx, `
INSERT INTO ussd_sessions(modem_id, initial_request, state, transcript)
VALUES (?, ?, 'active', ?)`, modemID, text, string(arr))
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
UPDATE ussd_sessions SET state = ?, ended_at = CURRENT_TIMESTAMP
WHERE modem_id = ? AND state IN ('active','user_response')`, state, modemID)
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
