package modem

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func TestDeleteOfflineModem_OnlyOffline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, err := s.UpsertModem(ctx, ModemState{DeviceID: "dev-delete", IMEI: "123456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOfflineModem(ctx, "dev-delete"); !errors.Is(err, ErrModemInUse) {
		t.Fatalf("expected ErrModemInUse, got %v", err)
	}
	if err := s.MarkModemAbsent(ctx, "dev-delete"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOfflineModem(ctx, "dev-delete"); err != nil {
		t.Fatalf("delete offline modem: %v", err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM modems WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected modem deleted, count=%d", n)
	}
	if err := s.DeleteOfflineModem(ctx, "dev-delete"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteUnusedSIM_OnlyUnbound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	modemID, err := s.UpsertModem(ctx, ModemState{DeviceID: "dev-sim-delete", IMEI: "223456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	simID, err := s.UpsertSim(ctx, SimState{ICCID: "8986000000000000001"}, modemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUnusedSIM(ctx, simID); !errors.Is(err, ErrSIMInUse) {
		t.Fatalf("expected ErrSIMInUse, got %v", err)
	}
	if err := s.UnbindModem(ctx, modemID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUnusedSIM(ctx, simID); err != nil {
		t.Fatalf("delete unused sim: %v", err)
	}
	if _, err := s.GetSIMByID(ctx, simID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected deleted sim not found, got %v", err)
	}
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
	shouldNotify, err := s.InsertSMS(ctx, rec, "devA", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("receiving sms should not notify")
	}
	// 再来一次，state=received + body 填上
	rec.State = "received"
	rec.Text = "hi"
	shouldNotify, err = s.InsertSMS(ctx, rec, "devA", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldNotify {
		t.Fatal("first received inbound should notify")
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
	var pushed int
	if err := s.db.QueryRowContext(ctx, `SELECT pushed_to_tg FROM sms`).Scan(&pushed); err != nil {
		t.Fatal(err)
	}
	if pushed != 1 {
		t.Errorf("expected pushed_to_tg=1 after notify, got %d", pushed)
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
	if _, err := s.InsertSMS(ctx, rec, "devX", modemID, simID); err != nil {
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

func TestInsertSMS_ContentDedupSuppressesAlreadyPushed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-content", IMEI: "900000000000003"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000003"}, modemID)
	ts := time.Date(2026, 4, 23, 14, 20, 35, 0, time.UTC)

	rec := SMSRecord{
		ExtID:     "/org/freedesktop/ModemManager1/SMS/1",
		Direction: "inbound",
		State:     "received",
		Peer:      "+49",
		Text:      "same body",
		Timestamp: ts,
	}
	shouldNotify, err := s.InsertSMS(ctx, rec, "dev-content", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldNotify {
		t.Fatal("first received inbound should notify")
	}

	rec.ExtID = "/org/freedesktop/ModemManager1/SMS/99"
	shouldNotify, err = s.InsertSMS(ctx, rec, "dev-content", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("content duplicate with pushed_to_tg=1 must not notify")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one deduped row, got %d", count)
	}
	var extID, sourceKey string
	var pushed int
	if err := s.db.QueryRowContext(ctx, `SELECT ext_id, source_key, pushed_to_tg FROM sms`).Scan(&extID, &sourceKey, &pushed); err != nil {
		t.Fatal(err)
	}
	if extID != rec.ExtID {
		t.Errorf("ext_id not updated: got %q want %q", extID, rec.ExtID)
	}
	wantSource := sourceKeyForSMS("dev-content", rec.ExtID)
	if sourceKey != wantSource {
		t.Errorf("source_key not updated: got %q want %q", sourceKey, wantSource)
	}
	if pushed != 1 {
		t.Errorf("pushed_to_tg should stay 1, got %d", pushed)
	}
}

func TestInsertSMS_ContentDedupBestEffortWithoutTimestamp(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-o2", IMEI: "900000000000004"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000004"}, modemID)
	rec := SMSRecord{
		ExtID:     "/mm/sms/10",
		Direction: "inbound",
		State:     "received",
		Peer:      "o2",
		Text:      "Ihr Code lautet 123456",
	}
	if shouldNotify, err := s.InsertSMS(ctx, rec, "dev-o2", modemID, simID); err != nil {
		t.Fatal(err)
	} else if !shouldNotify {
		t.Fatal("first no-timestamp sms should notify")
	}

	rec.ExtID = "/mm/sms/11"
	if shouldNotify, err := s.InsertSMS(ctx, rec, "dev-o2", modemID, simID); err != nil {
		t.Fatal(err)
	} else if shouldNotify {
		t.Fatal("same sim/peer/body without timestamp should best-effort dedup and suppress")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected best-effort dedup to one row, got %d", count)
	}
}

func TestInsertSMS_ContentDedupWithTimestampWithoutCachedIdentity(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-race", IMEI: "900000000000007"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000007"}, modemID)
	ts := time.Date(2026, 4, 23, 14, 20, 35, 0, time.UTC)
	rec := SMSRecord{ExtID: "/mm/sms/40", Direction: "inbound", State: "received", Peer: "o2 Preise", Text: "same timestamp body", Timestamp: ts}
	if err := s.UpsertSMS(ctx, rec, "dev-race", modemID, simID); err != nil {
		t.Fatal(err)
	}

	// 模拟启动/重枚举竞态：SMSReceived 早于 Runner modem/sim cache，就以 0 身份入库。
	rec.ExtID = "/mm/sms/41"
	shouldNotify, err := s.InsertSMS(ctx, rec, "dev-race", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("timestamp+peer+body global fallback should suppress historical duplicate without cached identity")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one deduped row, got %d", count)
	}
}

func TestInsertSMS_ContentDedupMergesSourceKeyTempRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-merge", IMEI: "900000000000008"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000008"}, modemID)
	ts := time.Date(2026, 4, 23, 14, 20, 35, 0, time.UTC)
	history := SMSRecord{ExtID: "/mm/sms/old", Direction: "inbound", State: "received", Peer: "o2 Preise", Text: "same historical body", Timestamp: ts}
	if err := s.UpsertSMS(ctx, history, "dev-merge", modemID, simID); err != nil {
		t.Fatal(err)
	}

	// 新 path 先以中间态出现并插入 source_key 临时行。
	tmp := SMSRecord{ExtID: "/mm/sms/new", Direction: "inbound", State: "receiving", Peer: "o2 Preise"}
	if err := s.UpsertSMS(ctx, tmp, "dev-merge", modemID, simID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("setup expected two rows, got %d", count)
	}

	// 后续完整 received/body 到达，应合并到历史内容行并删除临时 source_key 行。
	dup := history
	dup.ExtID = tmp.ExtID
	shouldNotify, err := s.InsertSMS(ctx, dup, "dev-merge", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("merged historical duplicate should not notify")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected temp row merged/deleted, got %d rows", count)
	}
	var extID string
	if err := s.db.QueryRowContext(ctx, `SELECT ext_id FROM sms`).Scan(&extID); err != nil {
		t.Fatal(err)
	}
	if extID != tmp.ExtID {
		t.Fatalf("expected merged row ext_id updated to %q, got %q", tmp.ExtID, extID)
	}
}

func TestInsertSMS_DuplicateBeforeNotifyCanNotifyOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-late", IMEI: "900000000000005"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000005"}, modemID)
	rec := SMSRecord{ExtID: "/mm/sms/20", Direction: "inbound", State: "received", Peer: "+1", Text: "late"}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO sms(sim_id, modem_id, direction, state, peer, body, ext_id, source_key, ts_created, pushed_to_tg)
VALUES (?, ?, 'inbound', 'received', ?, ?, ?, ?, ?, 0)`,
		simID, modemID, rec.Peer, rec.Text, rec.ExtID, sourceKeyForSMS("dev-late", rec.ExtID), nowRFC3339()); err != nil {
		t.Fatal(err)
	}

	shouldNotify, err := s.InsertSMS(ctx, rec, "dev-late", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldNotify {
		t.Fatal("existing unpushed received inbound should notify once")
	}
	shouldNotify, err = s.InsertSMS(ctx, rec, "dev-late", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("second duplicate after pushed_to_tg=1 should not notify")
	}
}

func TestUpsertSMS_SuppressesHistoricalReceivedNotification(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	modemID, _ := s.UpsertModem(ctx, ModemState{DeviceID: "dev-initial", IMEI: "900000000000006"})
	simID, _ := s.UpsertSim(ctx, SimState{ICCID: "8986900000000000006"}, modemID)
	rec := SMSRecord{ExtID: "/mm/sms/30", Direction: "inbound", State: "received", Peer: "+1", Text: "initial history"}
	if err := s.UpsertSMS(ctx, rec, "dev-initial", modemID, simID); err != nil {
		t.Fatal(err)
	}
	var pushed int
	if err := s.db.QueryRowContext(ctx, `SELECT pushed_to_tg FROM sms`).Scan(&pushed); err != nil {
		t.Fatal(err)
	}
	if pushed != 1 {
		t.Fatalf("UpsertSMS should mark historical received as already handled, pushed_to_tg=%d", pushed)
	}
	shouldNotify, err := s.InsertSMS(ctx, rec, "dev-initial", modemID, simID)
	if err != nil {
		t.Fatal(err)
	}
	if shouldNotify {
		t.Fatal("InsertSMS must not notify historical received already suppressed by UpsertSMS")
	}
}
