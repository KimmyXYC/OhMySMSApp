// store_reads.go —— HTTP/WS 层需要的只读查询。
//
// 读接口返回的行结构体带 JSON tag，httpapi 直接序列化即可；
// 与前端 types/api.ts 对齐（部分字段可能命名不同，前端 api 层可以轻微适配）。
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

// ---------- Row types ----------

// ModemRow 对应 modems 表 + 可选嵌套的 sim & latest signal。
type ModemRow struct {
	ID           int64    `json:"id"`
	DeviceID     string   `json:"device_id"`
	DBusPath     *string  `json:"dbus_path"`
	Manufacturer *string  `json:"manufacturer"`
	Model        *string  `json:"model"`
	Firmware     *string  `json:"firmware"`
	IMEI         *string  `json:"imei"`
	Plugin       *string  `json:"plugin"`
	PrimaryPort  *string  `json:"primary_port"`
	ATPorts      []string `json:"at_ports"`
	QMIPort      *string  `json:"qmi_port"`
	MBIMPort     *string  `json:"mbim_port"`
	USBPath      *string  `json:"usb_path"`
	Present      bool     `json:"present"`
	Nickname     *string  `json:"nickname"` // 用户自定义备注；NULL/空串表示未设置
	FirstSeenAt  string   `json:"first_seen_at"`
	LastSeenAt   string   `json:"last_seen_at"`

	SIM    *SimRow    `json:"sim,omitempty"`
	Signal *SignalRow `json:"signal,omitempty"`
}

// SimRow 对应 sims 表。
type SimRow struct {
	ID                  int64   `json:"id"`
	ICCID               string  `json:"iccid"`
	IMSI                *string `json:"imsi"`
	MSISDN              *string `json:"msisdn"`
	MSISDNOverride      *string `json:"msisdn_override,omitempty"`
	OperatorID          *string `json:"operator_id"`
	OperatorName        *string `json:"operator_name"`
	CardType            string  `json:"card_type"`
	ESIMCardID          *int64  `json:"esim_card_id"`
	ESIMProfileActive   bool    `json:"esim_profile_active"`
	ESIMProfileNickname *string `json:"esim_profile_nickname"`
	FirstSeenAt         string  `json:"first_seen_at"`
	LastSeenAt          string  `json:"last_seen_at"`
}

// SMSRow 对应 sms 表。
type SMSRow struct {
	ID           int64   `json:"id"`
	SimID        *int64  `json:"sim_id"`
	ModemID      *int64  `json:"modem_id"`
	Direction    string  `json:"direction"`
	State        string  `json:"state"`
	Peer         string  `json:"peer"`
	Body         string  `json:"body"`
	ExtID        *string `json:"ext_id"`
	TsReceived   *string `json:"ts_received"`
	TsCreated    string  `json:"ts_created"`
	TsSent       *string `json:"ts_sent"`
	ErrorMessage *string `json:"error_message"`
	PushedToTG   bool    `json:"pushed_to_tg"`
}

// USSDRow 对应 ussd_sessions 表。
type USSDRow struct {
	ID             int64           `json:"id"`
	SimID          *int64          `json:"sim_id"`
	ModemID        *int64          `json:"modem_id"`
	InitialRequest string          `json:"initial_request"`
	State          string          `json:"state"`
	Transcript     json.RawMessage `json:"transcript"`
	StartedAt      string          `json:"started_at"`
	EndedAt        *string         `json:"ended_at"`
}

// SignalRow 对应 signal_samples 表。
type SignalRow struct {
	ID           int64    `json:"id"`
	ModemID      int64    `json:"modem_id"`
	SimID        *int64   `json:"sim_id"`
	QualityPct   *int     `json:"quality_pct"`
	RSSIdBm      *int     `json:"rssi_dbm"`
	RSRPdBm      *int     `json:"rsrp_dbm"`
	RSRQdB       *int     `json:"rsrq_db"`
	SNRdB        *float64 `json:"snr_db"`
	AccessTech   *string  `json:"access_tech"`
	Registration *string  `json:"registration"`
	OperatorID   *string  `json:"operator_id"`
	OperatorName *string  `json:"operator_name"`
	SampledAt    string   `json:"sampled_at"`
}

// ThreadRow 聚合短信会话一行。
type ThreadRow struct {
	Peer      string `json:"peer"`
	SimID     *int64 `json:"sim_id"`
	LastText  string `json:"last_text"`
	LastTime  string `json:"last_time"`
	Count     int    `json:"count"`
	Direction string `json:"direction"`
	State     string `json:"state"`
}

// SMSFilter 列表过滤器。
type SMSFilter struct {
	SimID     int64
	DeviceID  string
	ModemID   int64
	Direction string // inbound / outbound / ""
	Peer      string
	Since     time.Time
	Limit     int
	Offset    int
}

// ---------- Queries ----------

const modemCols = `id, device_id, dbus_path, manufacturer, model, firmware, imei, plugin,
primary_port, at_ports, qmi_port, mbim_port, usb_path, present, nickname, first_seen_at, last_seen_at`

const simCols = `id, iccid, imsi, COALESCE(NULLIF(msisdn_override, ''), msisdn) AS msisdn, msisdn_override, operator_id, operator_name, card_type,
esim_card_id, esim_profile_active, esim_profile_nickname, first_seen_at, last_seen_at`

func scanModem(row interface {
	Scan(...any) error
}) (ModemRow, error) {
	var m ModemRow
	var atPortsJSON sql.NullString
	var presentInt int64
	err := row.Scan(
		&m.ID, &m.DeviceID, &m.DBusPath, &m.Manufacturer, &m.Model, &m.Firmware,
		&m.IMEI, &m.Plugin, &m.PrimaryPort, &atPortsJSON, &m.QMIPort, &m.MBIMPort,
		&m.USBPath, &presentInt, &m.Nickname, &m.FirstSeenAt, &m.LastSeenAt,
	)
	if err != nil {
		return m, err
	}
	m.Present = presentInt != 0
	if atPortsJSON.Valid && atPortsJSON.String != "" {
		_ = json.Unmarshal([]byte(atPortsJSON.String), &m.ATPorts)
	}
	return m, nil
}

func scanSim(row interface {
	Scan(...any) error
}) (SimRow, error) {
	var s SimRow
	var activeInt int64
	err := row.Scan(
		&s.ID, &s.ICCID, &s.IMSI, &s.MSISDN, &s.MSISDNOverride, &s.OperatorID, &s.OperatorName,
		&s.CardType, &s.ESIMCardID, &activeInt, &s.ESIMProfileNickname,
		&s.FirstSeenAt, &s.LastSeenAt,
	)
	if err != nil {
		return s, err
	}
	s.ESIMProfileActive = activeInt != 0
	return s, nil
}

// ListModems 返回所有 modem（含绑定的 SIM 与最近一次信号采样）。
func (s *Store) ListModems(ctx context.Context) ([]ModemRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+modemCols+` FROM modems ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModemRow
	for rows.Next() {
		m, err := scanModem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 填充 sim & signal
	for i := range out {
		sim, _ := s.simByModem(ctx, out[i].ID)
		out[i].SIM = sim
		sig, _ := s.latestSignal(ctx, out[i].ID)
		out[i].Signal = sig
	}
	return out, nil
}

// GetModemByDeviceID 通过 MM DeviceIdentifier 查询。不存在返回 sql.ErrNoRows。
func (s *Store) GetModemByDeviceID(ctx context.Context, deviceID string) (*ModemRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+modemCols+` FROM modems WHERE device_id = ?`, deviceID)
	m, err := scanModem(row)
	if err != nil {
		return nil, err
	}
	sim, _ := s.simByModem(ctx, m.ID)
	m.SIM = sim
	sig, _ := s.latestSignal(ctx, m.ID)
	m.Signal = sig
	return &m, nil
}

// GetModemByID 通过数据库 id 查询。
func (s *Store) GetModemByID(ctx context.Context, id int64) (*ModemRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+modemCols+` FROM modems WHERE id = ?`, id)
	m, err := scanModem(row)
	if err != nil {
		return nil, err
	}
	sim, _ := s.simByModem(ctx, m.ID)
	m.SIM = sim
	sig, _ := s.latestSignal(ctx, m.ID)
	m.Signal = sig
	return &m, nil
}

// simByModem 通过 modem_sim_bindings 查当前绑定 sim。
func (s *Store) simByModem(ctx context.Context, modemID int64) (*SimRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+simCols+` FROM sims
WHERE id = (SELECT sim_id FROM modem_sim_bindings WHERE modem_id = ?)`, modemID)
	sim, err := scanSim(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sim, nil
}

// latestSignal 查某 modem 最新一条 signal 采样。
func (s *Store) latestSignal(ctx context.Context, modemID int64) (*SignalRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, modem_id, sim_id, quality_pct, rssi_dbm, rsrp_dbm,
rsrq_db, snr_db, access_tech, registration, operator_id, operator_name, sampled_at
FROM signal_samples WHERE modem_id = ? ORDER BY id DESC LIMIT 1`, modemID)
	var sig SignalRow
	if err := row.Scan(
		&sig.ID, &sig.ModemID, &sig.SimID, &sig.QualityPct, &sig.RSSIdBm, &sig.RSRPdBm,
		&sig.RSRQdB, &sig.SNRdB, &sig.AccessTech, &sig.Registration, &sig.OperatorID, &sig.OperatorName,
		&sig.SampledAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sig, nil
}

// ListSIMs 返回所有 SIM 行。
func (s *Store) ListSIMs(ctx context.Context) ([]SimRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+simCols+` FROM sims ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SimRow
	for rows.Next() {
		sim, err := scanSim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sim)
	}
	return out, rows.Err()
}

// GetSIMByID 查询单个 SIM。
func (s *Store) GetSIMByID(ctx context.Context, id int64) (*SimRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+simCols+` FROM sims WHERE id = ?`, id)
	sim, err := scanSim(row)
	if err != nil {
		return nil, err
	}
	return &sim, nil
}

// ListSMS 查询短信列表（支持过滤）。
func (s *Store) ListSMS(ctx context.Context, f SMSFilter) ([]SMSRow, int, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"1=1"}
	args := []any{}
	if f.SimID > 0 {
		where = append(where, "sim_id = ?")
		args = append(args, f.SimID)
	}
	if f.ModemID > 0 {
		where = append(where, "modem_id = ?")
		args = append(args, f.ModemID)
	}
	if f.DeviceID != "" {
		where = append(where, "modem_id = (SELECT id FROM modems WHERE device_id = ?)")
		args = append(args, f.DeviceID)
	}
	if f.Direction != "" && f.Direction != "all" {
		where = append(where, "direction = ?")
		args = append(args, f.Direction)
	}
	if f.Peer != "" {
		where = append(where, "peer = ?")
		args = append(args, f.Peer)
	}
	if !f.Since.IsZero() {
		where = append(where, "ts_created >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339))
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sms WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	argsPaged := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, sim_id, modem_id, direction, state, peer, body, ext_id,
       ts_received, ts_created, ts_sent, error_message, pushed_to_tg
FROM sms WHERE `+whereSQL+` ORDER BY ts_created DESC, id DESC LIMIT ? OFFSET ?`, argsPaged...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SMSRow
	for rows.Next() {
		var r SMSRow
		var pushed int64
		if err := rows.Scan(&r.ID, &r.SimID, &r.ModemID, &r.Direction, &r.State, &r.Peer,
			&r.Body, &r.ExtID, &r.TsReceived, &r.TsCreated, &r.TsSent, &r.ErrorMessage, &pushed); err != nil {
			return nil, 0, err
		}
		r.PushedToTG = pushed != 0
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// GetSMSByID 查询单条。
func (s *Store) GetSMSByID(ctx context.Context, id int64) (*SMSRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, sim_id, modem_id, direction, state, peer, body, ext_id,
ts_received, ts_created, ts_sent, error_message, pushed_to_tg FROM sms WHERE id = ?`, id)
	var r SMSRow
	var pushed int64
	if err := row.Scan(&r.ID, &r.SimID, &r.ModemID, &r.Direction, &r.State, &r.Peer,
		&r.Body, &r.ExtID, &r.TsReceived, &r.TsCreated, &r.TsSent, &r.ErrorMessage, &pushed); err != nil {
		return nil, err
	}
	r.PushedToTG = pushed != 0
	return &r, nil
}

// DeleteSMSByID 删除一条短信。
func (s *Store) DeleteSMSByID(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sms WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListSMSThreads 按 peer 聚合。simID=0 时遍历所有 SIM。
func (s *Store) ListSMSThreads(ctx context.Context, simID int64) ([]ThreadRow, error) {
	// 先找每个 peer 的最近一条 id
	baseWhere := "sim_id IS NOT NULL"
	args := []any{}
	if simID > 0 {
		baseWhere = "sim_id = ?"
		args = append(args, simID)
	}
	q := fmt.Sprintf(`
SELECT s.peer, s.sim_id, s.body, s.ts_created, s.direction, s.state, cnt.c
FROM sms s
JOIN (
    SELECT peer, COALESCE(sim_id, -1) AS sk, MAX(id) AS maxid, COUNT(*) AS c
    FROM sms WHERE %s GROUP BY peer, sk
) cnt ON cnt.peer = s.peer AND COALESCE(s.sim_id, -1) = cnt.sk AND s.id = cnt.maxid
ORDER BY s.ts_created DESC, s.id DESC`, baseWhere)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThreadRow
	for rows.Next() {
		var t ThreadRow
		if err := rows.Scan(&t.Peer, &t.SimID, &t.LastText, &t.LastTime, &t.Direction, &t.State, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListUSSDSessions 返回 USSD 会话，最近的在前。
func (s *Store) ListUSSDSessions(ctx context.Context, limit int) ([]USSDRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, sim_id, modem_id, initial_request, state,
transcript, started_at, ended_at FROM ussd_sessions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []USSDRow
	for rows.Next() {
		var r USSDRow
		var transcript string
		if err := rows.Scan(&r.ID, &r.SimID, &r.ModemID, &r.InitialRequest, &r.State,
			&transcript, &r.StartedAt, &r.EndedAt); err != nil {
			return nil, err
		}
		r.Transcript = json.RawMessage(transcript)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetUSSDSessionByID 单条查询。
func (s *Store) GetUSSDSessionByID(ctx context.Context, id int64) (*USSDRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, sim_id, modem_id, initial_request, state,
transcript, started_at, ended_at FROM ussd_sessions WHERE id = ?`, id)
	var r USSDRow
	var transcript string
	if err := row.Scan(&r.ID, &r.SimID, &r.ModemID, &r.InitialRequest, &r.State,
		&transcript, &r.StartedAt, &r.EndedAt); err != nil {
		return nil, err
	}
	r.Transcript = json.RawMessage(transcript)
	return &r, nil
}

// CreateUSSDSession / UpdateUSSDSession 已删除：HTTP USSD handler 走事件 →
// runner.AppendUSSD + SetUSSDState 路径，不再需要手动建 session row。

// ListSignalHistory 返回某 modem 的信号采样历史（按 sampled_at DESC）。
func (s *Store) ListSignalHistory(ctx context.Context, modemID int64, since time.Time, limit int) ([]SignalRow, error) {
	if limit <= 0 {
		limit = 60
	}
	if limit > 1000 {
		limit = 1000
	}
	args := []any{modemID}
	q := `SELECT id, modem_id, sim_id, quality_pct, rssi_dbm, rsrp_dbm, rsrq_db, snr_db,
access_tech, registration, operator_id, operator_name, sampled_at
FROM signal_samples WHERE modem_id = ?`
	if !since.IsZero() {
		q += ` AND sampled_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SignalRow
	for rows.Next() {
		var r SignalRow
		if err := rows.Scan(&r.ID, &r.ModemID, &r.SimID, &r.QualityPct, &r.RSSIdBm, &r.RSRPdBm,
			&r.RSRQdB, &r.SNRdB, &r.AccessTech, &r.Registration, &r.OperatorID, &r.OperatorName, &r.SampledAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- settings ----------

// GetSetting 读 settings(key)。不存在返回 ""。
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// PutSetting upsert settings(key, value)。
func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}
