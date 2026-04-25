package esim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

// fakeCommander 是测试专用的 lpac 执行器，按 args[0:2] 分派预设响应。
type fakeCommander struct {
	mu       sync.Mutex
	calls    []fakeCall
	chipData json.RawMessage
	listData json.RawMessage
	// 启用/禁用切换会让 listData 跟着变；测试可设置 toggleHook
	toggleHook func(args []string)
	// 强制错误：subcmd → exit code & detail
	forceErr map[string]struct {
		Code int
		Msg  string
	}
}

type fakeCall struct {
	Args []string
	Env  []string
}

func (f *fakeCommander) run(_ context.Context, _ string, args []string, env []string) ([]byte, []byte, int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Args: append([]string(nil), args...), Env: append([]string(nil), env...)})
	f.mu.Unlock()
	if len(args) < 2 {
		return nil, nil, 1, errors.New("bad args")
	}
	key := args[0] + " " + args[1]
	if fe, ok := f.forceErr[key]; ok {
		// 模拟 lpac 输出 code != 0 帧
		envBody := map[string]any{
			"type": "lpa",
			"payload": map[string]any{
				"code":    fe.Code,
				"message": fe.Msg,
				"data":    nil,
			},
		}
		raw, _ := json.Marshal(envBody)
		return append(raw, '\n'), nil, 0, nil
	}

	if f.toggleHook != nil {
		f.toggleHook(args)
	}

	var data json.RawMessage
	switch key {
	case "chip info":
		data = f.chipData
	case "profile list":
		data = f.listData
	case "profile enable", "profile disable", "profile nickname", "profile download", "profile delete":
		data = json.RawMessage(`null`)
	default:
		return nil, nil, 1, errors.New("unknown subcommand")
	}
	envBody := map[string]any{
		"type": "lpa",
		"payload": map[string]any{
			"code":    0,
			"message": "success",
			"data":    json.RawMessage(data),
		},
	}
	raw, _ := json.Marshal(envBody)
	return append(raw, '\n'), nil, 0, nil
}

// fakeInhibitor 是 inhibit/uninhibit 的桩，记录调用。可让 inhibit 失败。
type fakeInhibitor struct {
	mu       sync.Mutex
	inhibits []string
	releases []string
	failOn   map[string]bool
}

type resetProvider struct {
	deviceIDs []string
}

func (p *resetProvider) Start(context.Context) error               { return nil }
func (p *resetProvider) Events() <-chan modem.Event                { return nil }
func (p *resetProvider) ListModems() []modem.ModemState            { return nil }
func (p *resetProvider) GetModem(string) (modem.ModemState, bool)  { return modem.ModemState{}, false }
func (p *resetProvider) ListSMS(string) ([]modem.SMSRecord, error) { return nil, nil }
func (p *resetProvider) SendSMS(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (p *resetProvider) DeleteSMS(context.Context, string, string) error { return nil }
func (p *resetProvider) InitiateUSSD(context.Context, string, string) (string, string, error) {
	return "", "", nil
}
func (p *resetProvider) RespondUSSD(context.Context, string, string) (string, error) { return "", nil }
func (p *resetProvider) CancelUSSD(context.Context, string) error                    { return nil }
func (p *resetProvider) ResetModem(_ context.Context, deviceID string) error {
	p.deviceIDs = append(p.deviceIDs, deviceID)
	return nil
}

func (f *fakeInhibitor) inhibit(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn[uid] {
		return ErrInhibitFailed
	}
	f.inhibits = append(f.inhibits, uid)
	return nil
}

func (f *fakeInhibitor) uninhibit(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases = append(f.releases, uid)
	return nil
}

// 替换 Service.inhibit 用 fakeInhibitor 的小适配器：
// 直接覆盖字段级方法不可行，所以包一个 thinInhibit 接口，临时给 Service 加 hook。
//
// 为了避免改动产品代码，我们用反射方式不可行，干脆在测试里替换 s.inhibit 字段。
// inhibitor 的 inhibit/uninhibit 方法都用值类型（pointer），所以直接 swap 一个
// "假的" *inhibitor 即可——不行，inhibitor 内部走 DBus，在没 system bus 的测试环境下
// 一定失败。改为：让 Service 只暴露 inhibitFunc/uninhibitFunc 字段（指向 inhibitor 方法），
// 测试可替换。

// --- 真正的测试 ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestService(t *testing.T) (*Service, *fakeCommander, *sql.DB, *modem.Store, int64) {
	t.Helper()
	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	store := modem.NewStore(conn)

	// 写一个 modem，带 qmi_port 和 usb_path
	mState := modem.ModemState{
		DeviceID:     "dev_test_modem_1",
		DBusPath:     "/org/freedesktop/ModemManager1/Modem/0",
		Manufacturer: "Quectel",
		Model:        "EC25",
		IMEI:         "111122223333444",
		USBPath:      "/sys/devices/test/usb1/1-1",
		Ports: []modem.Port{
			{Name: "cdc-wdm3", Type: "qmi"},
			{Name: "ttyUSB2", Type: "at"},
		},
		State:  modem.ModemStateRegistered,
		HasSim: true,
		SIM: &modem.SimState{
			ICCID: "894921007608614852",
		},
	}
	modemID, err := store.UpsertModem(ctx, mState)
	if err != nil {
		t.Fatalf("upsert modem: %v", err)
	}
	if _, err := store.UpsertSim(ctx, *mState.SIM, modemID); err != nil {
		t.Fatalf("upsert sim: %v", err)
	}

	// 写一个 lpac stub 二进制（让 available() 返回 true）。内容不重要，
	// 因为我们走 fakeCommander 不真正 exec。
	lpacBin := filepath.Join(tmp, "lpac")
	if err := os.WriteFile(lpacBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake lpac: %v", err)
	}

	cfg := config.ESIMConfig{
		LPACBin:          lpacBin,
		LPACDriversDir:   tmp,
		OperationTimeout: 10 * time.Second,
		DiscoverCooldown: time.Hour,
	}
	auditSvc := audit.New(conn, discardLogger())
	svc := New(cfg, conn, store, nil, nil, auditSvc, discardLogger())

	// 注入 fakeCommander
	fc := &fakeCommander{
		chipData: json.RawMessage(`{
			"eidValue":"35840574202500000125000004509296",
			"EUICCInfo2":{
				"profileVersion":"2.2.2",
				"euiccFirmwareVer":"1.2.3",
				"extCardResource":{"freeNonVolatileMemory":12345}
			}
		}`),
		listData: json.RawMessage(`[
			{"iccid":"894921007608614852","isdpAid":"A0000005591010FFFFFFFF8900001100",
			 "profileState":"enabled","profileNickname":"",
			 "serviceProviderName":"o2-de","profileName":"o2-de",
			 "profileClass":"operational"}
		]`),
		forceErr: map[string]struct {
			Code int
			Msg  string
		}{},
	}
	svc.lpac.cmd = fc

	// 替换 inhibitor：原 inhibitor 走 DBus，测试环境必定失败。
	// 我们用一个不实际调用 DBus、只记录的版本（直接覆盖 active 计数路径不够，
	// inhibit() 内部一上来就 ConnectSystemBus）。
	// 解决：把 inhibit 字段换成一个永远短路成功的实现 —— 通过把 lpac.cmd 的执行
	// 同时保留 inhibitor 的引用计数行为（只为通过测试），把 active 预填 → 0 计数
	// 直接进 inhibit 方法时会跳过 DBus 调用。
	//
	// 简单做法：直接把 svc.inhibit 替换为一个零状态 inhibitor，并在每次 inhibit
	// 调用前预先 active[uid]=1 让它直接命中"已 inhibit"分支。但这反而会让真正的
	// inhibit 调用没记录，违背语义。
	//
	// 最干净的做法：把 inhibitor 抽成接口。这里采取最小改动：直接替换 inhibit/
	// uninhibit 方法，方式为给 Service 添加可选的 inhibitOverride（见下面）。
	// 测试使用 svc.testOverrideInhibit。
	fi := &fakeInhibitor{failOn: map[string]bool{}}
	svc.inhibitOverrideInhibit = fi.inhibit
	svc.inhibitOverrideUninhibit = fi.uninhibit
	svc.postToggleDelay = 0
	svc.modemResetDelay = 0
	t.Cleanup(func() { svc.Stop() })

	return svc, fc, conn, store, modemID
}

func TestEnableProfile_AlreadyEnabled(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()

	// 先 Discover 让 card / profile 进 DB
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	// 此时 profile 是 enabled 状态；再 enable 应当 ErrNoChangeNeeded
	_, err := svc.EnableProfile(ctx, "894921007608614852")
	if !errors.Is(err, ErrNoChangeNeeded) {
		t.Fatalf("expected ErrNoChangeNeeded, got %v", err)
	}
	_ = fc
}

func TestDisableThenEnable_HappyPath(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}

	// disable：把 listData 切换为 disabled，模拟 lpac 执行后状态
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "disable" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","profileState":"disabled",
				 "serviceProviderName":"o2-de","profileName":"o2-de",
				 "profileClass":"operational"}
			]`)
		}
		if len(args) >= 2 && args[0] == "profile" && args[1] == "enable" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","profileState":"enabled",
				 "serviceProviderName":"o2-de","profileName":"o2-de",
				 "profileClass":"operational"}
			]`)
		}
	}

	payload, err := svc.DisableProfile(ctx, "894921007608614852")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if payload["prev_state"] != "enabled" || payload["new_state"] != "disabled" {
		t.Fatalf("payload mismatch: %+v", payload)
	}

	// 现在 enable
	payload2, err := svc.EnableProfile(ctx, "894921007608614852")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if payload2["prev_state"] != "disabled" {
		t.Errorf("expected prev_state=disabled, got %v", payload2["prev_state"])
	}
}

func TestToggleProfile_RequestsModemReset(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	rp := &resetProvider{}
	svc.provider = rp
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "disable" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","profileState":"disabled",
				 "serviceProviderName":"o2-de","profileName":"o2-de"}
			]`)
		}
	}
	payload, err := svc.DisableProfile(ctx, "894921007608614852")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if payload["modem_refresh"] != "reset_requested" {
		t.Fatalf("expected reset_requested payload, got %+v", payload)
	}
	if len(rp.deviceIDs) != 1 || rp.deviceIDs[0] != "dev_test_modem_1" {
		t.Fatalf("expected reset for dev_test_modem_1, got %+v", rp.deviceIDs)
	}
}

func TestEnableProfile_DisablesCurrentBeforeTarget(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	fc.listData = json.RawMessage(`[
		{"iccid":"894921007608614852","isdpAid":"A0000005591010FFFFFFFF8900001000","profileState":"enabled",
		 "serviceProviderName":"o2-de","profileName":"o2-de"},
		{"iccid":"8931440400000000001","isdpAid":"A0000005591010FFFFFFFF8900001100","profileState":"disabled",
		 "serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
	]`)
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	fc.toggleHook = func(args []string) {
		if len(args) >= 3 && args[0] == "profile" && args[1] == "disable" && args[2] == "A0000005591010FFFFFFFF8900001000" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","isdpAid":"A0000005591010FFFFFFFF8900001000","profileState":"disabled",
				 "serviceProviderName":"o2-de","profileName":"o2-de"},
				{"iccid":"8931440400000000001","isdpAid":"A0000005591010FFFFFFFF8900001100","profileState":"disabled",
				 "serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
			]`)
		}
		if len(args) >= 3 && args[0] == "profile" && args[1] == "enable" && args[2] == "A0000005591010FFFFFFFF8900001100" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","isdpAid":"A0000005591010FFFFFFFF8900001000","profileState":"disabled",
				 "serviceProviderName":"o2-de","profileName":"o2-de"},
				{"iccid":"8931440400000000001","isdpAid":"A0000005591010FFFFFFFF8900001100","profileState":"enabled",
				 "serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
			]`)
		}
	}
	payload, err := svc.EnableProfile(ctx, "8931440400000000001")
	if err != nil {
		t.Fatalf("enable target: %v", err)
	}
	if payload["previous_enabled_iccid"] != "894921007608614852" {
		t.Fatalf("expected previous enabled in payload, got %+v", payload)
	}

	var sawDisable, sawEnable bool
	for _, c := range fc.calls {
		if len(c.Args) >= 3 && c.Args[0] == "profile" && c.Args[1] == "disable" && c.Args[2] == "A0000005591010FFFFFFFF8900001000" {
			sawDisable = true
		}
		if len(c.Args) >= 3 && c.Args[0] == "profile" && c.Args[1] == "enable" && c.Args[2] == "A0000005591010FFFFFFFF8900001100" {
			sawEnable = true
		}
	}
	if !sawDisable || !sawEnable {
		t.Fatalf("expected disable current then enable target, sawDisable=%v sawEnable=%v calls=%+v", sawDisable, sawEnable, fc.calls)
	}
}

func TestGetCardRefreshesProfilesFromChip(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE esim_profiles SET last_refreshed_at = ? WHERE card_id = 1`, "2000-01-01T00:00:00Z"); err != nil {
		t.Fatalf("force stale cache: %v", err)
	}
	fc.mu.Lock()
	before := len(fc.calls)
	fc.mu.Unlock()
	fc.listData = json.RawMessage(`[
		{"iccid":"894921007608614852","profileState":"disabled",
		 "serviceProviderName":"o2-de","profileName":"o2-de"},
		{"iccid":"8931440400000000001","profileState":"enabled",
		 "serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
	]`)
	detail, err := svc.GetCard(ctx, 1)
	if err != nil {
		t.Fatalf("get card: %v", err)
	}
	if detail.ActiveICCID == nil || *detail.ActiveICCID != "8931440400000000001" {
		t.Fatalf("expected active ICCID from fresh chip list, got %+v", detail.ActiveICCID)
	}
	foundList := false
	fc.mu.Lock()
	for _, c := range fc.calls[before:] {
		if len(c.Args) >= 2 && c.Args[0] == "profile" && c.Args[1] == "list" {
			foundList = true
		}
	}
	fc.mu.Unlock()
	if !foundList {
		t.Fatalf("expected GetCard to run profile list")
	}
}

func TestEnableProfile_LPACError(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	// 预切到 disabled 状态：用 hook 让 disable 后的 list refresh 返回 disabled
	disabledList := json.RawMessage(`[
		{"iccid":"894921007608614852","profileState":"disabled",
		 "serviceProviderName":"o2-de","profileName":"o2-de"}
	]`)
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "disable" {
			fc.listData = disabledList
		}
	}
	if _, err := svc.DisableProfile(ctx, "894921007608614852"); err != nil {
		t.Fatalf("setup disable: %v", err)
	}
	fc.toggleHook = nil

	fc.forceErr["profile enable"] = struct {
		Code int
		Msg  string
	}{Code: -1, Msg: "ES10c.EnableProfile failed: notInLink"}

	_, err := svc.EnableProfile(ctx, "894921007608614852")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrLPACError) {
		t.Errorf("expected ErrLPACError, got %v", err)
	}
	var lerr *LPACError
	if !errors.As(err, &lerr) {
		t.Fatal("expected *LPACError")
	}
	if lerr.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

func TestEnableProfile_NotFound(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	_, err := svc.EnableProfile(context.Background(), "9999999999999999999")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected ErrProfileNotFound, got %v", err)
	}
}

func TestEnableProfile_InhibitFails(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	disabledList := json.RawMessage(`[
		{"iccid":"894921007608614852","profileState":"disabled",
		 "serviceProviderName":"o2-de","profileName":"o2-de"}
	]`)
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "disable" {
			fc.listData = disabledList
		}
	}
	if _, err := svc.DisableProfile(ctx, "894921007608614852"); err != nil {
		t.Fatalf("setup disable: %v", err)
	}
	fc.toggleHook = nil

	// 让 inhibit 失败
	svc.inhibitOverrideInhibit = func(_ context.Context, _ string) error {
		return ErrInhibitFailed
	}
	_, err := svc.EnableProfile(ctx, "894921007608614852")
	if !errors.Is(err, ErrInhibitFailed) {
		t.Fatalf("expected ErrInhibitFailed, got %v", err)
	}
	_ = fc
}

func TestAddProfile_WithActivationCode(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "download" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","profileState":"enabled","serviceProviderName":"o2-de","profileName":"o2-de"},
				{"iccid":"8931440400000000001","profileState":"disabled","serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
			]`)
		}
	}
	detail, payload, err := svc.AddProfile(ctx, 1, AddProfileRequest{
		ActivationCode: "LPA:1$example.invalid$TEST-MATCHING-ID",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if payload["method"] != "activation_code" {
		t.Fatalf("payload method mismatch: %+v", payload)
	}
	if len(detail.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(detail.Profiles))
	}
	foundDownload := false
	for _, c := range fc.calls {
		if len(c.Args) >= 4 && c.Args[0] == "profile" && c.Args[1] == "download" && c.Args[2] == "-a" {
			foundDownload = true
		}
	}
	if !foundDownload {
		t.Fatalf("expected profile download -a call, calls=%+v", fc.calls)
	}
}

func TestDeleteProfile_DisabledOnly(t *testing.T) {
	svc, fc, _, _, modemID := newTestService(t)
	ctx := context.Background()
	fc.listData = json.RawMessage(`[
		{"iccid":"894921007608614852","profileState":"enabled","serviceProviderName":"o2-de","profileName":"o2-de"},
		{"iccid":"8931440400000000001","profileState":"disabled","serviceProviderName":"BetterRoaming","profileName":"BetterRoaming"}
	]`)
	if err := svc.runDiscoverOnModem(ctx, modemID); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if _, err := svc.DeleteProfile(ctx, "894921007608614852", "o2-de"); !errors.Is(err, ErrProfileActive) {
		t.Fatalf("expected ErrProfileActive, got %v", err)
	}
	if _, err := svc.DeleteProfile(ctx, "8931440400000000001", "wrong name"); !errors.Is(err, ErrInvalidProfileInput) {
		t.Fatalf("expected ErrInvalidProfileInput, got %v", err)
	}
	fc.toggleHook = func(args []string) {
		if len(args) >= 2 && args[0] == "profile" && args[1] == "delete" {
			fc.listData = json.RawMessage(`[
				{"iccid":"894921007608614852","profileState":"enabled","serviceProviderName":"o2-de","profileName":"o2-de"}
			]`)
		}
	}
	if _, err := svc.DeleteProfile(ctx, "8931440400000000001", "BetterRoaming"); err != nil {
		t.Fatalf("delete disabled: %v", err)
	}
	if _, err := svc.profileByICCID(ctx, "8931440400000000001"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected deleted profile not found, got %v", err)
	}
}

func TestParseChipInfo(t *testing.T) {
	raw := json.RawMessage(`{
		"eidValue":"89086030202200000024000011265376",
		"EUICCInfo2":{"profileVersion":"2.2.2","euiccFirmwareVer":"5.4.3",
		              "extCardResource":{"freeNonVolatileMemory":4096}}
	}`)
	ci, err := parseChipInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ci.EID != "89086030202200000024000011265376" {
		t.Errorf("eid: %s", ci.EID)
	}
	if ci.ProfileVersion != "2.2.2" {
		t.Errorf("pv: %s", ci.ProfileVersion)
	}
	if ci.EUICCFirmware != "5.4.3" {
		t.Errorf("fw: %s", ci.EUICCFirmware)
	}
	if ci.FreeNVM != 4096 {
		t.Errorf("nvm: %d", ci.FreeNVM)
	}
}

func TestParseProfileList(t *testing.T) {
	raw := json.RawMessage(`[
		{"iccid":"111","profileState":"enabled","profileName":"a","serviceProviderName":"sp"},
		{"iccid":"222","profileState":"disabled","profileNickname":"backup"},
		{"profileState":"enabled"}
	]`)
	out, err := parseProfileList(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 (no-iccid dropped), got %d", len(out))
	}
	if out[0].State != "enabled" || out[1].State != "disabled" {
		t.Errorf("states: %+v", out)
	}
	if out[1].Nickname != "backup" {
		t.Errorf("nickname: %+v", out[1])
	}
}
