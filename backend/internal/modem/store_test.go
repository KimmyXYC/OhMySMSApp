package modem

import (
	"context"
	"testing"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
)

// newTestStore 建一个临时 SQLite 库并 apply 所有迁移，返回 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	// :memory: 不走 file path，但 db.Open 会 MkdirAll(filepath.Dir)；用 t.TempDir 做个真文件更贴近生产。
	dbPath := t.TempDir() + "/test.db"
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewStore(conn)
}

// TestUpsertModem_IMEIStableAcrossDeviceIDChange 验证核心修复：
// 同一 IMEI 的模块即使 device_id 变化（换 USB 口 / 固件切身份），
// 也只产生一行，不会再生成幽灵。
func TestUpsertModem_IMEIStableAcrossDeviceIDChange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// 第一次：Huawei ME906s 在原 USB 口
	m1 := ModemState{
		DeviceID:     "dev_aaaaaaaaaaaaaa_huawei_me906s",
		DBusPath:     "/org/freedesktop/ModemManager1/Modem/0",
		Manufacturer: "Huawei",
		Model:        "ME906s",
		IMEI:         "867223020359329",
		USBPath:      "/sys/devices/.../1-1.4",
	}
	id1, err := s.UpsertModem(ctx, m1)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if id1 == 0 {
		t.Fatal("id1 = 0")
	}

	// 第二次：同一物理模块，换到另一个 USB 口，MM 重新探测为 ME909s-821（device_id 变）
	m2 := ModemState{
		DeviceID:     "dev_bbbbbbbbbbbbbb_huawei_me909s",
		DBusPath:     "/org/freedesktop/ModemManager1/Modem/3",
		Manufacturer: "Huawei",
		Model:        "ME909s-821",
		IMEI:         "867223020359329", // 同一 IMEI
		USBPath:      "/sys/devices/.../1-1.2",
	}
	id2, err := s.UpsertModem(ctx, m2)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected same modem id, got id1=%d id2=%d (ghost created)", id1, id2)
	}

	// 验证 DB 里只有一行 & device_id / model 已被新值覆盖
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM modems`).Scan(&count); err != nil {
		t.Fatalf("count modems: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 modem row, got %d", count)
	}

	var gotDeviceID, gotModel, gotIMEI string
	var present int
	err = s.db.QueryRowContext(ctx,
		`SELECT device_id, model, imei, present FROM modems WHERE id = ?`, id1,
	).Scan(&gotDeviceID, &gotModel, &gotIMEI, &present)
	if err != nil {
		t.Fatalf("select modem: %v", err)
	}
	if gotDeviceID != m2.DeviceID {
		t.Errorf("device_id not updated: got %q want %q", gotDeviceID, m2.DeviceID)
	}
	if gotModel != "ME909s-821" {
		t.Errorf("model not updated: %q", gotModel)
	}
	if gotIMEI != "867223020359329" {
		t.Errorf("imei mismatch: %q", gotIMEI)
	}
	if present != 1 {
		t.Errorf("present should be 1, got %d", present)
	}
}

// TestUpsertModem_EmptyIMEIFallsBackToDeviceID 验证 IMEI 为空时退回 device_id 冲突键，
// 两个不同 device_id 的"未识别 IMEI 模块"能并存（不被错误合并）。
func TestUpsertModem_EmptyIMEIFallsBackToDeviceID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	m1 := ModemState{DeviceID: "dev_noimei_1", Model: "unknown-1"}
	m2 := ModemState{DeviceID: "dev_noimei_2", Model: "unknown-2"}

	id1, err := s.UpsertModem(ctx, m1)
	if err != nil {
		t.Fatalf("upsert m1: %v", err)
	}
	id2, err := s.UpsertModem(ctx, m2)
	if err != nil {
		t.Fatalf("upsert m2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("two empty-IMEI modems must not merge: id1=id2=%d", id1)
	}

	// 再次 upsert m1（device_id 冲突键），应复用 id1
	m1Again := m1
	m1Again.Model = "unknown-1-updated"
	idAgain, err := s.UpsertModem(ctx, m1Again)
	if err != nil {
		t.Fatalf("re-upsert m1: %v", err)
	}
	if idAgain != id1 {
		t.Fatalf("re-upsert by device_id should reuse id: got %d want %d", idAgain, id1)
	}
}

// TestUpsertModem_SamePhysicalDifferentIMEI_CoExist 验证两个不同 IMEI 的模块独立存在。
func TestUpsertModem_SamePhysicalDifferentIMEI_CoExist(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, err := s.UpsertModem(ctx, ModemState{DeviceID: "d1", IMEI: "111111111111111"})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.UpsertModem(ctx, ModemState{DeviceID: "d2", IMEI: "222222222222222"})
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatalf("different IMEI must produce different rows")
	}
}

// TestMarkStaleModemsAbsent 验证幽灵清扫：
// - 不在 seenDeviceIDs 且 present=1 → 置 0 并解绑
// - 在 seenDeviceIDs 中 → 保持 present=1
// - 已 present=0 的不受影响
// - seenDeviceIDs 为空 → no-op
func TestMarkStaleModemsAbsent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// 三行：seen / stale(present=1) / already-absent
	idSeen, _ := s.UpsertModem(ctx, ModemState{DeviceID: "seen-d", IMEI: "10000000000001"})
	idStale, _ := s.UpsertModem(ctx, ModemState{DeviceID: "stale-d", IMEI: "10000000000002"})
	idAbsent, _ := s.UpsertModem(ctx, ModemState{DeviceID: "absent-d", IMEI: "10000000000003"})
	// 手动把 absent-d 改成 present=0
	if _, err := s.db.ExecContext(ctx,
		`UPDATE modems SET present = 0 WHERE id = ?`, idAbsent); err != nil {
		t.Fatal(err)
	}

	// 给 stale 一个 sim 绑定，验证解绑生效
	simID, err := s.UpsertSim(ctx, SimState{ICCID: "8986000000000000001", IMSI: "460010000000001"}, idStale)
	if err != nil {
		t.Fatal(err)
	}
	if simID == 0 {
		t.Fatal("sim not inserted")
	}

	// seenDeviceIDs 为空 → no-op
	n, err := s.MarkStaleModemsAbsent(ctx, nil)
	if err != nil {
		t.Fatalf("sweep empty: %v", err)
	}
	if n != 0 {
		t.Errorf("empty sweep should affect 0 rows, got %d", n)
	}

	// 正式扫除：只保留 seen-d
	n, err = s.MarkStaleModemsAbsent(ctx, []string{"seen-d"})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row marked absent, got %d", n)
	}

	// 验证状态
	mustPresent := func(id int64, want int) {
		t.Helper()
		var p int
		if err := s.db.QueryRowContext(ctx,
			`SELECT present FROM modems WHERE id = ?`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		if p != want {
			t.Errorf("modem id=%d present=%d want=%d", id, p, want)
		}
	}
	mustPresent(idSeen, 1)
	mustPresent(idStale, 0)
	mustPresent(idAbsent, 0)

	// 验证 stale 的 sim 绑定已清
	var bindings int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM modem_sim_bindings WHERE modem_id = ?`, idStale,
	).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Errorf("stale modem binding not cleaned: %d rows", bindings)
	}
}

// TestUpsertSim_EmptyICCIDSkipped 验证无 ICCID 时不再合成 "imsi:<IMSI>"；
// 直接返回 (0, nil)，不向 sims 表写任何行。
func TestUpsertSim_EmptyICCIDSkipped(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "d1", IMEI: "111222333444555"})
	simID, err := s.UpsertSim(ctx, SimState{IMSI: "460010000000009", OperatorName: "CMCC"}, modemID)
	if err != nil {
		t.Fatalf("upsert sim: %v", err)
	}
	if simID != 0 {
		t.Errorf("expected simID=0 for empty ICCID, got %d", simID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sims`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 sim rows, got %d", n)
	}
}

// TestInsertSMS_SourceKeyDedup 验证 source_key 去重：
// 同 (deviceID, ext_id) 多次插入只产生一行，state 会更新到最新。
func TestInsertSMS_SourceKeyDedup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "devA", IMEI: "900000000000001"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000001"}, modemID)

	rec := SMSRecord{
		ExtID:     "/org/freedesktop/ModemManager1/SMS/7",
		Direction: "inbound",
		State:     "receiving",
		Peer:      "+1",
		Text:      "",
	}
	if err := s.InsertSMS(ctx, rec, "devA", modemID, simID); err != nil {
		t.Fatal(err)
	}
	// 再来一次，state=received + body 填上
	rec.State = "received"
	rec.Text = "hi"
	if err := s.InsertSMS(ctx, rec, "devA", modemID, simID); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected dedup to 1 row, got %d", count)
	}
	var state, body string
	if err := s.db.QueryRowContext(ctx, `SELECT state, body FROM sms`).Scan(&state, &body); err != nil {
		t.Fatal(err)
	}
	if state != "received" || body != "hi" {
		t.Errorf("expected state=received body=hi, got state=%q body=%q", state, body)
	}
}

// TestUpdateSMSState_BySourceKey 验证新的 UpdateSMSState 签名按 (deviceID, extID) 定位。
func TestUpdateSMSState_BySourceKey(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "devX", IMEI: "900000000000002"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000002"}, modemID)
	rec := SMSRecord{
		ExtID: "/mm/sms/9", Direction: "outbound", State: "sending",
		Peer: "+2", Text: "ok",
	}
	if err := s.InsertSMS(ctx, rec, "devX", modemID, simID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSMSState(ctx, "devX", "/mm/sms/9", "sent", ""); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM sms`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "sent" {
		t.Errorf("expected state=sent, got %q", got)
	}
}
