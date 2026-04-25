package esim

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/audit"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
)

const profileCacheTTL = 15 * time.Second
const modemRecoveryTimeout = 15 * time.Second
const modemRecoveryPoll = 500 * time.Millisecond

// Service 是 esim 子系统的对外门面。
//
// 职责：
//   - 维护 esim_cards / esim_profiles 两张表
//   - 通过 lpac + ModemManager InhibitDevice 完成 enable/disable/nickname 操作
//   - 订阅 modem.Runner 事件，对 newly registered 的 modem 自动跑一次 chip info
//
// profile 下载、删除等写操作均需 inhibit MM 并串行化，避免 APDU 通道抢占。
type Service struct {
	cfg      config.ESIMConfig
	db       *sql.DB
	store    *modem.Store
	provider modem.Provider
	runner   *modem.Runner
	audit    *audit.Service
	log      *slog.Logger

	lpac    *lpacRunner
	inhibit *inhibitor

	// 测试钩子：覆盖 inhibit/uninhibit 行为，避免在没 system bus 的环境下尝试连接。
	// 生产代码不设置；nil 时走真正的 inhibitor。
	inhibitOverrideInhibit   func(ctx context.Context, uid string) error
	inhibitOverrideUninhibit func(ctx context.Context, uid string) error

	// postToggleDelay 在 enable/disable 之后 sleep 让 eUICC 应用变更。
	// 默认 2s；测试可置 0。
	postToggleDelay time.Duration
	// modemResetDelay 在 uninhibit 后等待 MM 重新接管 modem，再请求 Reset。
	// 默认 3s；测试可置 0。
	modemResetDelay time.Duration

	// 进程内串行：同一 modem 不允许并发跑 lpac，避免抢占同一 cdc-wdm。
	modemLockMu sync.Mutex
	modemLocks  map[int64]*sync.Mutex

	// 自动发现冷却：modem.id → 最近一次跑 chip info 的时间。
	discoverMu       sync.Mutex
	discoverLastSeen map[int64]time.Time

	// 后台 goroutine 控制
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// New 构造 Service。db / store / provider / runner 必填；audit 可为 nil。
func New(cfg config.ESIMConfig, db *sql.DB, store *modem.Store, provider modem.Provider,
	runner *modem.Runner, auditSvc *audit.Service, log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		cfg:              cfg,
		db:               db,
		store:            store,
		provider:         provider,
		runner:           runner,
		audit:            auditSvc,
		log:              log,
		lpac:             newLPACRunner(cfg.LPACBin, cfg.LPACDriversDir, cfg.OperationTimeout),
		inhibit:          newInhibitor(),
		modemLocks:       make(map[int64]*sync.Mutex),
		discoverLastSeen: make(map[int64]time.Time),
		postToggleDelay:  2 * time.Second,
		modemResetDelay:  3 * time.Second,
	}
}

// Start 启动后台自动发现订阅。可重复调用：第二次会先 Stop。
func (s *Service) Start(parent context.Context) {
	s.Stop()
	if s.runner == nil {
		s.log.Info("esim service: no runner, auto-discover disabled")
		return
	}
	if !s.lpac.available() {
		s.log.Warn("esim service: lpac binary not available, auto-discover disabled",
			"bin", s.cfg.LPACBin)
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.bgCtx = ctx
	s.bgCancel = cancel
	events, unsub := s.runner.Subscribe(64)
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		defer unsub()
		s.autoDiscoverLoop(ctx, events)
	}()
	s.log.Info("esim service started", "lpac_bin", s.cfg.LPACBin,
		"drivers_dir", s.cfg.LPACDriversDir)
}

// Stop 停止后台 goroutine 并关闭 DBus 连接。
func (s *Service) Stop() {
	if s.bgCancel != nil {
		s.bgCancel()
		s.bgWG.Wait()
		s.bgCancel = nil
	}
	if s.inhibit != nil {
		s.inhibit.close()
	}
}

// modemLock 拿/创建某 modem 的进程内串行锁。
func (s *Service) modemLock(modemID int64) *sync.Mutex {
	s.modemLockMu.Lock()
	defer s.modemLockMu.Unlock()
	m, ok := s.modemLocks[modemID]
	if !ok {
		m = &sync.Mutex{}
		s.modemLocks[modemID] = m
	}
	return m
}

// doInhibit / doUninhibit 走测试 override（如有），否则走真实 DBus inhibitor。
func (s *Service) doInhibit(ctx context.Context, uid string) error {
	if s.inhibitOverrideInhibit != nil {
		return s.inhibitOverrideInhibit(ctx, uid)
	}
	return s.inhibit.inhibit(ctx, uid)
}

func (s *Service) doUninhibit(ctx context.Context, uid string) error {
	if s.inhibitOverrideUninhibit != nil {
		return s.inhibitOverrideUninhibit(ctx, uid)
	}
	return s.inhibit.uninhibit(ctx, uid)
}

// ---------------- Public API ----------------

// ListCards 列出所有已发现 eUICC。
func (s *Service) ListCards(ctx context.Context) ([]ESimCard, error) {
	return s.queryCards(ctx, 0)
}

// GetCard 查一张 card 详情（含 profiles）。
func (s *Service) GetCard(ctx context.Context, id int64) (*ESimCardDetail, error) {
	if s.profilesFresh(ctx, id, profileCacheTTL) {
		return s.getCardCached(ctx, id)
	}
	if err := s.refreshCardProfilesFromChip(ctx, id); err != nil {
		if detail, derr := s.getCardCached(ctx, id); derr == nil && len(detail.Profiles) > 0 {
			return detail, nil
		}
		return nil, err
	}
	return s.getCardCached(ctx, id)
}

// getCardCached 只读 DB 缓存，不触发 lpac。内部用于 Discover 等已经刷新过芯片的路径。
func (s *Service) getCardCached(ctx context.Context, id int64) (*ESimCardDetail, error) {
	cards, err := s.queryCards(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, ErrCardNotFound
	}
	profiles, err := s.queryProfiles(ctx, id)
	if err != nil {
		return nil, err
	}
	if profiles == nil {
		profiles = []ESimProfile{}
	}
	return &ESimCardDetail{ESimCard: cards[0], Profiles: profiles}, nil
}

// ListProfiles 列出某 card 的 profile。
func (s *Service) ListProfiles(ctx context.Context, cardID int64) ([]ESimProfile, error) {
	if !s.profilesFresh(ctx, cardID, profileCacheTTL) {
		if err := s.refreshCardProfilesFromChip(ctx, cardID); err != nil {
			profiles, qerr := s.queryProfiles(ctx, cardID)
			if qerr == nil && len(profiles) > 0 {
				return profiles, nil
			}
			return nil, err
		}
	}
	return s.queryProfiles(ctx, cardID)
}

// SetCardNickname 仅前端备注（不写 eUICC）。空字符串清空。
func (s *Service) SetCardNickname(ctx context.Context, cardID int64, nickname string) error {
	nickname = strings.TrimSpace(nickname)
	var arg any
	if nickname == "" {
		arg = nil
	} else {
		arg = nickname
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE esim_cards SET nickname = ? WHERE id = ?`, arg, cardID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCardNotFound
	}
	return nil
}

// Discover 强制对 cardID 重新跑 chip info + profile list 并 upsert DB。
//
// 当 card 还未绑定 modem 时，遍历当前在线、有 SIM 的所有 modem 跑 chip info，
// 找出 EID 匹配的那个并绑定。
func (s *Service) Discover(ctx context.Context, cardID int64) (*ESimCardDetail, error) {
	if !s.lpac.available() {
		return nil, ErrLPACUnavailable
	}
	card, err := s.cardByID(ctx, cardID)
	if err != nil {
		return nil, err
	}
	modemID := int64(0)
	if card.ModemID != nil {
		modemID = *card.ModemID
	}
	if modemID == 0 {
		// 反查：尝试每个在线 modem
		boundID, _, err := s.findBearer(ctx, card.EID)
		if err != nil {
			return nil, err
		}
		if boundID == 0 {
			return nil, ErrModemNotBound
		}
		modemID = boundID
	}

	if err := s.runDiscoverOnModem(ctx, modemID); err != nil {
		return nil, err
	}
	return s.getCardCached(ctx, cardID)
}

// EnableProfile 启用某 profile（按 ICCID）。
func (s *Service) EnableProfile(ctx context.Context, iccid string) (auditPayload map[string]any, err error) {
	return s.toggleProfile(ctx, iccid, true)
}

// DisableProfile 禁用 profile。
func (s *Service) DisableProfile(ctx context.Context, iccid string) (auditPayload map[string]any, err error) {
	return s.toggleProfile(ctx, iccid, false)
}

// AddProfileRequest 描述一次 eSIM profile 下载/安装请求。
// 可直接传 QR 内容（ActivationCode，如 LPA:1$...），也可手动传 SM-DP+ 与 Matching ID。
type AddProfileRequest struct {
	ActivationCode   string
	SMDPAddress      string
	MatchingID       string
	ConfirmationCode string
}

// AddProfile 下载/安装一个 profile 到指定 eUICC card。
func (s *Service) AddProfile(ctx context.Context, cardID int64, req AddProfileRequest) (*ESimCardDetail, map[string]any, error) {
	if !s.lpac.available() {
		return nil, nil, ErrLPACUnavailable
	}
	req.ActivationCode = strings.TrimSpace(req.ActivationCode)
	req.SMDPAddress = strings.TrimSpace(req.SMDPAddress)
	req.MatchingID = strings.TrimSpace(req.MatchingID)
	req.ConfirmationCode = strings.TrimSpace(req.ConfirmationCode)
	if req.ActivationCode == "" && (req.SMDPAddress == "" || req.MatchingID == "") {
		return nil, nil, fmt.Errorf("%w: provide activation_code or smdp_address + matching_id", ErrInvalidProfileInput)
	}
	card, modemRow, t, err := s.lookupCardAndModem(ctx, cardID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.runProfileWrite(ctx, *card.ModemID, strDeref(modemRow.USBPath), t,
		profileDownloadCmd(req.ActivationCode, req.SMDPAddress, req.MatchingID, req.ConfirmationCode),
		nil,
		func() error {
			if data, e := s.lpac.runJSON(ctx, t, chipInfoCmd()); e == nil {
				if ci, perr := parseChipInfo(data); perr == nil {
					_, _ = s.upsertCard(ctx, ci.EID, ci, *card.ModemID)
				}
			}
			data, e := s.lpac.runJSON(ctx, t, profileListCmd())
			if e != nil {
				return e
			}
			entries, perr := parseProfileList(data)
			if perr != nil {
				return perr
			}
			s.upsertProfiles(ctx, card.ID, entries)
			return nil
		},
	); err != nil {
		return nil, nil, err
	}
	detail, err := s.GetCard(ctx, card.ID)
	if err != nil {
		return nil, nil, err
	}
	payload := map[string]any{
		"eid":             card.EID,
		"card_id":         card.ID,
		"modem_device_id": modemRow.DeviceID,
		"transport":       t.Kind,
		"method":          "manual",
	}
	if req.ActivationCode != "" {
		payload["method"] = "activation_code"
		payload["activation_code_len"] = len(req.ActivationCode)
	} else {
		payload["smdp_address"] = req.SMDPAddress
	}
	return detail, payload, nil
}

// DeleteProfile 删除一个 disabled profile。enabled profile 必须先禁用，避免误删当前可用配置。
func (s *Service) DeleteProfile(ctx context.Context, iccid, confirmName string) (map[string]any, error) {
	if !s.lpac.available() {
		return nil, ErrLPACUnavailable
	}
	prof, card, modemRow, t, err := s.lookupProfileAndModem(ctx, iccid)
	if err != nil {
		return nil, err
	}
	profileName := profileDisplayName(prof)
	if strings.TrimSpace(confirmName) != profileName {
		return nil, fmt.Errorf("%w: confirmation name does not match", ErrInvalidProfileInput)
	}
	if prof.State == ProfileStateEnabled {
		return nil, ErrProfileActive
	}
	if err := s.runProfileWrite(ctx, *card.ModemID, strDeref(modemRow.USBPath), t,
		profileDeleteCmd(iccid),
		func() error {
			fresh, ferr := s.profileByICCID(ctx, iccid)
			if ferr != nil {
				return ferr
			}
			if fresh.State == ProfileStateEnabled {
				return ErrProfileActive
			}
			return nil
		},
		func() error {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM esim_profiles WHERE iccid = ?`, iccid); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx, `
UPDATE sims
SET esim_card_id = NULL,
    esim_profile_active = 0,
    esim_profile_nickname = NULL,
    card_type = 'psim'
WHERE iccid = ?`, iccid); err != nil {
				return err
			}
			if data, e := s.lpac.runJSON(ctx, t, profileListCmd()); e == nil {
				if entries, perr := parseProfileList(data); perr == nil {
					s.upsertProfiles(ctx, card.ID, entries)
				}
			} else {
				s.log.Warn("post-delete profile list refresh failed", "iccid", iccid, "err", e)
			}
			return nil
		},
	); err != nil {
		return nil, err
	}
	return map[string]any{
		"eid":              card.EID,
		"iccid":            iccid,
		"modem_device_id":  modemRow.DeviceID,
		"transport":        t.Kind,
		"profile_name":     derefStr(prof.ProfileName),
		"service_provider": derefStr(prof.ServiceProvider),
		"nickname":         derefStr(prof.Nickname),
	}, nil
}

// SetProfileNickname 修改 profile 在 eUICC 上的 nickname（写卡）。
func (s *Service) SetProfileNickname(ctx context.Context, iccid, nickname string) (map[string]any, error) {
	if !s.lpac.available() {
		return nil, ErrLPACUnavailable
	}
	prof, card, modemRow, t, err := s.lookupProfileAndModem(ctx, iccid)
	if err != nil {
		return nil, err
	}
	lock := s.modemLock(*card.ModemID)
	lock.Lock()
	defer lock.Unlock()

	uid := strDeref(modemRow.USBPath)
	if uid == "" {
		return nil, fmt.Errorf("%w: modem has no usb_path uid", ErrInhibitFailed)
	}
	if err := s.doInhibit(ctx, uid); err != nil {
		return nil, err
	}
	defer func() { _ = s.doUninhibit(context.Background(), uid) }()

	args := profileNicknameCmd(iccid, nickname)
	if _, e := s.lpac.runJSON(ctx, t, args); e != nil {
		return nil, e
	}
	// 同步 DB
	_, _ = s.db.ExecContext(ctx,
		`UPDATE esim_profiles SET nickname = ?, last_refreshed_at = ? WHERE iccid = ?`,
		nullable(nickname), nowRFC3339(), iccid)

	payload := map[string]any{
		"eid":             card.EID,
		"iccid":           iccid,
		"modem_device_id": modemRow.DeviceID,
		"transport":       t.Kind,
		"prev_nickname":   derefStr(prof.Nickname),
		"new_nickname":    nickname,
	}
	return payload, nil
}

// ---------------- internal helpers ----------------

// toggleProfile 是 enable/disable 的共同实现。enable=true 启用，false 禁用。
// 写操作必须以芯片 profile list 为准：进入锁和 inhibit 后先 fresh list；失败后也
// 尽力 fresh list，避免把 DB 缓存当成真实芯片状态。
func (s *Service) toggleProfile(ctx context.Context, iccid string, enable bool) (map[string]any, error) {
	if !s.lpac.available() {
		return nil, ErrLPACUnavailable
	}
	prof, card, modemRow, t, err := s.lookupProfileAndModem(ctx, iccid)
	if err != nil {
		return nil, err
	}
	wantState := ProfileStateDisabled
	if enable {
		wantState = ProfileStateEnabled
	}

	lock := s.modemLock(*card.ModemID)
	lock.Lock()
	defer lock.Unlock()

	uid := strDeref(modemRow.USBPath)
	if uid == "" {
		return nil, fmt.Errorf("%w: modem has no usb_path uid", ErrInhibitFailed)
	}
	if err := s.doInhibit(ctx, uid); err != nil {
		return nil, err
	}
	uninhibited := false
	defer func() {
		if !uninhibited {
			_ = s.doUninhibit(context.Background(), uid)
		}
	}()

	entries, err := s.refreshProfilesLocked(ctx, card.ID, t)
	if err != nil {
		return nil, err
	}
	target, ok := findProfileEntry(entries, iccid)
	if !ok {
		return nil, ErrProfileNotFound
	}
	prevState := target.State
	current, hasCurrent := enabledProfileEntry(entries)

	if target.State == wantState {
		return nil, ErrNoChangeNeeded
	}

	if enable && hasCurrent && current.ICCID != target.ICCID {
		if _, lerr := s.lpac.runJSON(ctx, t, profileDisableCmd(profileOpID(current))); lerr != nil {
			_, _ = s.refreshProfilesLocked(ctx, card.ID, t)
			return nil, lerr
		}
		if s.postToggleDelay > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(s.postToggleDelay):
			}
		}
		entries, err = s.refreshProfilesLocked(ctx, card.ID, t)
		if err != nil {
			return nil, err
		}
		if cur, stillEnabled := enabledProfileEntry(entries); stillEnabled && cur.ICCID == current.ICCID {
			return nil, fmt.Errorf("%w: current profile %s could not be disabled", ErrLPACError, current.ICCID)
		}
	}

	var opErr error
	if enable {
		_, opErr = s.lpac.runJSON(ctx, t, profileEnableCmd(profileOpID(target)))
	} else {
		_, opErr = s.lpac.runJSON(ctx, t, profileDisableCmd(profileOpID(target)))
	}
	if opErr != nil {
		_, _ = s.refreshProfilesLocked(ctx, card.ID, t)
		// 如果 switch 中已经禁用了原 current，但启用目标失败，尽力回滚到原 current。
		if enable && hasCurrent && current.ICCID != target.ICCID {
			if _, rerr := s.lpac.runJSON(ctx, t, profileEnableCmd(profileOpID(current))); rerr != nil {
				s.log.Warn("rollback enable previous profile failed", "previous_iccid", current.ICCID, "err", rerr)
			}
			_, _ = s.refreshProfilesLocked(ctx, card.ID, t)
		}
		return nil, opErr
	}

	// 给 eUICC 一点时间应用变更
	if s.postToggleDelay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(s.postToggleDelay):
		}
	}

	if _, e := s.refreshProfilesLocked(ctx, card.ID, t); e != nil {
		s.log.Warn("post-toggle profile list refresh failed",
			"iccid", iccid, "err", e)
	}

	// uninhibit 让 MM 重新上线 modem
	if err := s.doUninhibit(ctx, uid); err == nil {
		uninhibited = true
	} else {
		return nil, err
	}
	refreshStatus := "uninhibited"
	var refreshErr string
	if s.provider != nil {
		if s.modemResetDelay > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(s.modemResetDelay):
			}
		}
		if err := s.provider.ResetModem(ctx, modemRow.DeviceID); err != nil {
			refreshStatus = "reset_failed"
			refreshErr = err.Error()
			s.log.Warn("post-toggle modem reset failed", "device_id", modemRow.DeviceID, "err", err)
		} else {
			refreshStatus = "reset_requested"
		}
	}

	payload := map[string]any{
		"eid":             card.EID,
		"iccid":           iccid,
		"modem_device_id": modemRow.DeviceID,
		"transport":       t.Kind,
		"profile_aid":     target.ISDPAid,
		"prev_state":      prevState,
		"new_state":       wantState,
		"modem_refresh":   refreshStatus,
	}
	if refreshErr != "" {
		payload["modem_refresh_error"] = refreshErr
	}
	if enable && hasCurrent && current.ICCID != target.ICCID {
		payload["previous_enabled_iccid"] = current.ICCID
		payload["previous_enabled_aid"] = current.ISDPAid
	}
	_ = prof
	return payload, nil
}

// lookupProfileAndModem 是大多数写操作的前置：DB 查 profile/card，
// 校验 card 已绑定到一个在线 modem，并解析 transport。
func (s *Service) lookupProfileAndModem(ctx context.Context, iccid string) (
	ESimProfile, ESimCard, *modem.ModemRow, transportInfo, error,
) {
	prof, err := s.profileByICCID(ctx, iccid)
	if err != nil {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, err
	}
	cards, err := s.queryCards(ctx, prof.CardID)
	if err != nil {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, err
	}
	if len(cards) == 0 {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, ErrCardNotFound
	}
	card := cards[0]
	if card.ModemID == nil {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, ErrModemNotBound
	}
	mrow, err := s.store.GetModemByID(ctx, *card.ModemID)
	if err != nil {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, err
	}
	if mrow == nil || !mrow.Present {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, ErrModemOffline
	}
	t, ok := resolveTransport(mrow)
	if !ok {
		return ESimProfile{}, ESimCard{}, nil, transportInfo{}, ErrTransportUnsupported
	}
	return prof, card, mrow, t, nil
}

// lookupCardAndModem 查 card 并确认已绑定在线 modem。
func (s *Service) lookupCardAndModem(ctx context.Context, cardID int64) (ESimCard, *modem.ModemRow, transportInfo, error) {
	card, err := s.cardByID(ctx, cardID)
	if err != nil {
		return ESimCard{}, nil, transportInfo{}, err
	}
	if card.ModemID == nil {
		return ESimCard{}, nil, transportInfo{}, ErrModemNotBound
	}
	mrow, err := s.store.GetModemByID(ctx, *card.ModemID)
	if err != nil {
		return ESimCard{}, nil, transportInfo{}, err
	}
	if mrow == nil || !mrow.Present {
		if err := s.waitModemRecovery(ctx, *card.ModemID, modemRecoveryTimeout); err != nil {
			return ESimCard{}, nil, transportInfo{}, ErrModemOffline
		}
		mrow, err = s.store.GetModemByID(ctx, *card.ModemID)
		if err != nil {
			return ESimCard{}, nil, transportInfo{}, err
		}
		if mrow == nil || !mrow.Present {
			return ESimCard{}, nil, transportInfo{}, ErrModemOffline
		}
	}
	t, ok := resolveTransport(mrow)
	if !ok {
		return ESimCard{}, nil, transportInfo{}, ErrTransportUnsupported
	}
	return card, mrow, t, nil
}

// runProfileWrite 串行执行一次会写 eUICC 的 lpac 命令，并在成功后执行 after。
func (s *Service) runProfileWrite(ctx context.Context, modemID int64, uid string, t transportInfo, args []string, before func() error, after func() error) error {
	if uid == "" {
		return fmt.Errorf("%w: modem has no usb_path uid", ErrInhibitFailed)
	}
	lock := s.modemLock(modemID)
	lock.Lock()
	defer lock.Unlock()
	if err := s.doInhibit(ctx, uid); err != nil {
		return err
	}
	unInhibited := false
	defer func() {
		if !unInhibited {
			_ = s.doUninhibit(context.Background(), uid)
		}
	}()
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	if _, err := s.lpac.runJSON(ctx, t, args); err != nil {
		return err
	}
	if s.postToggleDelay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(s.postToggleDelay):
		}
	}
	if after != nil {
		if err := after(); err != nil {
			return err
		}
	}
	if err := s.doUninhibit(ctx, uid); err == nil {
		unInhibited = true
	}
	return nil
}

// findBearer 遍历当前在线 modem，跑 chip info，找到 EID 匹配的那个。
// 找到后会 upsert 绑定关系并返回 modemID。
func (s *Service) findBearer(ctx context.Context, eid string) (int64, transportInfo, error) {
	if eid == "" {
		return 0, transportInfo{}, ErrCardNotFound
	}
	rows, err := s.store.ListModems(ctx)
	if err != nil {
		return 0, transportInfo{}, err
	}
	for i := range rows {
		mrow := &rows[i]
		if !mrow.Present {
			continue
		}
		if mrow.SIM == nil {
			continue
		}
		t, ok := resolveTransport(mrow)
		if !ok {
			continue
		}
		uid := strDeref(mrow.USBPath)
		if uid == "" {
			continue
		}
		// inhibit + run + uninhibit
		eidGot, _, err := s.runChipInfoLocked(ctx, mrow.ID, uid, t)
		if err != nil {
			s.log.Debug("findBearer: chip info failed",
				"modem_id", mrow.ID, "err", err)
			continue
		}
		if strings.EqualFold(eidGot, eid) {
			// 绑定！
			_, _ = s.db.ExecContext(ctx,
				`UPDATE esim_cards SET modem_id = ?, last_seen_at = ? WHERE eid = ?`,
				mrow.ID, nowRFC3339(), eidGot)
			return mrow.ID, t, nil
		}
	}
	return 0, transportInfo{}, ErrModemNotBound
}

// runDiscoverOnModem 对一个已绑定 modem 跑 chip info + profile list，并 upsert DB。
func (s *Service) runDiscoverOnModem(ctx context.Context, modemID int64) error {
	mrow, err := s.store.GetModemByID(ctx, modemID)
	if err != nil {
		return err
	}
	if mrow == nil || !mrow.Present {
		return ErrModemOffline
	}
	t, ok := resolveTransport(mrow)
	if !ok {
		return ErrTransportUnsupported
	}
	uid := strDeref(mrow.USBPath)
	if uid == "" {
		return fmt.Errorf("%w: modem has no usb_path uid", ErrInhibitFailed)
	}

	// 一次 inhibit 包住 chip info + profile list + DB upsert，避免中间 uninhibit 后
	// MM 重新枚举 modem 导致 second inhibit 报 "Modem not exported in the bus"。
	lock := s.modemLock(modemID)
	lock.Lock()
	defer lock.Unlock()

	if err := s.doInhibit(ctx, uid); err != nil {
		return err
	}
	defer func() { _ = s.doUninhibit(context.Background(), uid) }()

	// 1. chip info → 拿到 EID + euicc 元数据
	rawChip, err := s.lpac.runJSON(ctx, t, chipInfoCmd())
	if err != nil {
		return err
	}
	ci, err := parseChipInfo(rawChip)
	if err != nil {
		return err
	}
	if ci.EID == "" {
		return errors.New("chip info: empty EID")
	}

	// 2. upsert card（不需要 inhibit，纯 DB 操作；放锁内确保和后续 profile list 写入一致）
	cardID, err := s.upsertCard(ctx, ci.EID, ci, modemID)
	if err != nil {
		return err
	}

	// 3. profile list → upsert profiles
	rawList, err := s.lpac.runJSON(ctx, t, profileListCmd())
	if err != nil {
		return err
	}
	entries, err := parseProfileList(rawList)
	if err != nil {
		return err
	}
	s.upsertProfiles(ctx, cardID, entries)
	return nil
}

// runChipInfoLocked 拿 modem 锁、inhibit、跑 chip info、uninhibit、解析 EID。
func (s *Service) runChipInfoLocked(ctx context.Context, modemID int64, uid string, t transportInfo) (string, chipInfoData, error) {
	lock := s.modemLock(modemID)
	lock.Lock()
	defer lock.Unlock()

	if err := s.doInhibit(ctx, uid); err != nil {
		return "", chipInfoData{}, err
	}
	defer func() { _ = s.doUninhibit(context.Background(), uid) }()

	data, err := s.lpac.runJSON(ctx, t, chipInfoCmd())
	if err != nil {
		return "", chipInfoData{}, err
	}
	ci, err := parseChipInfo(data)
	if err != nil {
		return "", chipInfoData{}, err
	}
	return ci.EID, ci, nil
}

// runProfileListAndUpsert 拿锁、inhibit、profile list、uninhibit、写库。
func (s *Service) runProfileListAndUpsert(ctx context.Context, cardID, modemID int64, uid string, t transportInfo) error {
	lock := s.modemLock(modemID)
	lock.Lock()
	defer lock.Unlock()

	if err := s.doInhibit(ctx, uid); err != nil {
		return err
	}
	uninhibited := false
	defer func() {
		if !uninhibited {
			_ = s.doUninhibit(context.Background(), uid)
		}
	}()

	data, err := s.lpac.runJSON(ctx, t, profileListCmd())
	if err != nil {
		return err
	}
	entries, err := parseProfileList(data)
	if err != nil {
		return err
	}
	s.upsertProfiles(ctx, cardID, entries)
	if err := s.doUninhibit(ctx, uid); err != nil {
		return err
	}
	uninhibited = true
	return s.waitModemRecovery(ctx, modemID, modemRecoveryTimeout)
}

func (s *Service) refreshProfilesLocked(ctx context.Context, cardID int64, t transportInfo) ([]profileEntry, error) {
	data, err := s.lpac.runJSON(ctx, t, profileListCmd())
	if err != nil {
		return nil, err
	}
	entries, err := parseProfileList(data)
	if err != nil {
		return nil, err
	}
	s.upsertProfiles(ctx, cardID, entries)
	return entries, nil
}

func findProfileEntry(entries []profileEntry, iccid string) (profileEntry, bool) {
	iccid = modem.NormalizeICCID(iccid)
	for _, p := range entries {
		if p.ICCID == iccid {
			return p, true
		}
	}
	return profileEntry{}, false
}

func enabledProfileEntry(entries []profileEntry) (profileEntry, bool) {
	for _, p := range entries {
		if p.State == ProfileStateEnabled {
			return p, true
		}
	}
	return profileEntry{}, false
}

func profileOpID(p profileEntry) string {
	if p.ISDPAid != "" {
		return p.ISDPAid
	}
	return p.ICCID
}

func (s *Service) profilesFresh(ctx context.Context, cardID int64, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	var ts sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT MAX(last_refreshed_at)
FROM esim_profiles
WHERE card_id = ?`, cardID).Scan(&ts)
	if err != nil || !ts.Valid || ts.String == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return false
	}
	return time.Since(t) < ttl
}

func (s *Service) waitModemRecovery(ctx context.Context, modemID int64, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = modemRecoveryTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		mrow, err := s.store.GetModemByID(ctx, modemID)
		if err == nil && mrow != nil && mrow.Present {
			if _, ok := resolveTransport(mrow); ok {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return ErrModemOffline
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(modemRecoveryPoll):
		}
	}
}

// refreshCardProfilesFromChip 强制从 eUICC 芯片读取 profile list，并把结果同步到 DB。
// 这是对外 profile/详情接口的数据源，DB 只作为刚刷新后的查询/关联缓存。
func (s *Service) refreshCardProfilesFromChip(ctx context.Context, cardID int64) error {
	if !s.lpac.available() {
		return ErrLPACUnavailable
	}
	card, mrow, t, err := s.lookupCardAndModem(ctx, cardID)
	if err != nil {
		return err
	}
	uid := strDeref(mrow.USBPath)
	if uid == "" {
		return fmt.Errorf("%w: modem has no usb_path uid", ErrInhibitFailed)
	}
	return s.runProfileListAndUpsert(ctx, card.ID, *card.ModemID, uid, t)
}

// upsertCard upsert 一条 esim_cards 行。
func (s *Service) upsertCard(ctx context.Context, eid string, ci chipInfoData, modemID int64) (int64, error) {
	now := nowRFC3339()
	vendor := vendorFromEID(eid)

	_, err := s.db.ExecContext(ctx, `
INSERT INTO esim_cards(eid, vendor, euicc_firmware, profile_version, free_nvm,
                       modem_id, last_seen_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(eid) DO UPDATE SET
    vendor          = COALESCE(NULLIF(excluded.vendor, 'unknown'), esim_cards.vendor),
    euicc_firmware  = COALESCE(NULLIF(excluded.euicc_firmware, ''), esim_cards.euicc_firmware),
    profile_version = COALESCE(NULLIF(excluded.profile_version, ''), esim_cards.profile_version),
    free_nvm        = COALESCE(excluded.free_nvm, esim_cards.free_nvm),
    modem_id        = excluded.modem_id,
    last_seen_at    = excluded.last_seen_at
`,
		eid, vendor,
		nullable(ci.EUICCFirmware), nullable(ci.ProfileVersion),
		nullableI64(ci.FreeNVM),
		modemID, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert esim_card: %w", err)
	}
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM esim_cards WHERE eid = ?`, eid).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// upsertProfiles 把 lpac profile list 的结果写入 DB；同时更新 sims.esim_card_id 链接。
func (s *Service) upsertProfiles(ctx context.Context, cardID int64, entries []profileEntry) {
	now := nowRFC3339()
	for _, p := range entries {
		_, err := s.db.ExecContext(ctx, `
INSERT INTO esim_profiles(card_id, iccid, isdp_aid, state, nickname,
                          service_provider, profile_name, profile_class,
                          last_refreshed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(iccid) DO UPDATE SET
    card_id           = excluded.card_id,
    isdp_aid          = COALESCE(NULLIF(excluded.isdp_aid, ''), esim_profiles.isdp_aid),
    state             = excluded.state,
    nickname          = COALESCE(NULLIF(excluded.nickname, ''), esim_profiles.nickname),
    service_provider  = COALESCE(NULLIF(excluded.service_provider, ''), esim_profiles.service_provider),
    profile_name      = COALESCE(NULLIF(excluded.profile_name, ''), esim_profiles.profile_name),
    profile_class     = COALESCE(NULLIF(excluded.profile_class, ''), esim_profiles.profile_class),
    last_refreshed_at = excluded.last_refreshed_at
`,
			cardID, p.ICCID,
			nullable(p.ISDPAid), p.State, nullable(p.Nickname),
			nullable(p.ServiceProvider), nullable(p.ProfileName),
			nullable(p.ProfileClass),
			now,
		)
		if err != nil {
			s.log.Warn("upsert esim_profile failed",
				"iccid", p.ICCID, "err", err)
			continue
		}
		// 链接到 sims（如果该 ICCID 已经被 modem 看到）
		_, _ = s.db.ExecContext(ctx, `
UPDATE sims SET esim_card_id = ?,
                esim_profile_active = ?,
                esim_profile_nickname = ?,
                card_type = 'sticker_esim'
WHERE iccid = ?`,
			cardID,
			boolToInt(p.State == ProfileStateEnabled),
			nullable(p.Nickname),
			p.ICCID,
		)
	}
}

// autoDiscoverLoop 订阅 runner 事件，对首次 registered + 有 SIM 的 modem 跑 chip info。
func (s *Service) autoDiscoverLoop(ctx context.Context, events <-chan modem.Event) {
	cooldown := s.cfg.DiscoverCooldown
	if cooldown < minDiscoverCooldown {
		cooldown = minDiscoverCooldown
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.maybeAutoDiscover(ctx, ev, cooldown)
		}
	}
}

func (s *Service) maybeAutoDiscover(ctx context.Context, ev modem.Event, cooldown time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("esim auto-discover panic", "panic", r)
		}
	}()

	switch ev.Kind {
	case modem.EventModemAdded, modem.EventModemUpdated:
	default:
		return
	}
	state, ok := ev.Payload.(modem.ModemState)
	if !ok {
		return
	}
	// 仅对 registered 且有 SIM 的 modem 触发
	if state.SIM == nil {
		return
	}
	if state.State != modem.ModemStateRegistered &&
		state.State != modem.ModemStateConnected &&
		state.State != modem.ModemStateEnabled {
		return
	}

	// 通过 deviceID 查 modem.id
	mrow, err := s.store.GetModemByDeviceID(ctx, state.DeviceID)
	if err != nil || mrow == nil {
		return
	}
	if !mrow.Present {
		return
	}
	if _, ok := resolveTransport(mrow); !ok {
		return
	}

	// cooldown
	s.discoverMu.Lock()
	last := s.discoverLastSeen[mrow.ID]
	if !last.IsZero() && time.Since(last) < cooldown {
		s.discoverMu.Unlock()
		return
	}
	s.discoverLastSeen[mrow.ID] = time.Now()
	s.discoverMu.Unlock()

	s.log.Info("esim auto-discover starting",
		"modem_id", mrow.ID, "device_id", state.DeviceID)
	if err := s.runDiscoverOnModem(ctx, mrow.ID); err != nil {
		s.log.Warn("esim auto-discover failed",
			"modem_id", mrow.ID, "device_id", state.DeviceID, "err", err)
		// 失败时把时间戳回退一半，给下次重试机会
		s.discoverMu.Lock()
		s.discoverLastSeen[mrow.ID] = time.Now().Add(-cooldown / 2)
		s.discoverMu.Unlock()
	} else {
		s.log.Info("esim auto-discover ok", "modem_id", mrow.ID)
	}
}

// ---------------- DB queries ----------------

// queryCards 按 cardID（0=全部）查询 cards，并 join modem 信息 + active profile。
func (s *Service) queryCards(ctx context.Context, cardID int64) ([]ESimCard, error) {
	q := `
SELECT
    c.id, c.eid, c.vendor, c.nickname, c.notes,
    c.euicc_firmware, c.profile_version, c.free_nvm,
    c.modem_id, c.last_seen_at, c.created_at,
    m.device_id, m.model, m.qmi_port, m.mbim_port,
    p.iccid, COALESCE(NULLIF(p.profile_name,''), p.service_provider) AS active_name
FROM esim_cards c
LEFT JOIN modems m ON m.id = c.modem_id
LEFT JOIN esim_profiles p ON p.card_id = c.id AND p.state = 'enabled'
`
	args := []any{}
	if cardID > 0 {
		q += ` WHERE c.id = ?`
		args = append(args, cardID)
	}
	q += ` ORDER BY c.id`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ESimCard{}
	for rows.Next() {
		var c ESimCard
		var notes sql.NullString
		var firmware, pver sql.NullString
		var freeNVM sql.NullInt64
		var modemID sql.NullInt64
		var lastSeen sql.NullString
		var modemDev, modemModel, qmi, mbim sql.NullString
		var activeICCID, activeName sql.NullString

		if err := rows.Scan(
			&c.ID, &c.EID, &c.Vendor, &c.Nickname, &notes,
			&firmware, &pver, &freeNVM,
			&modemID, &lastSeen, &c.CreatedAt,
			&modemDev, &modemModel, &qmi, &mbim,
			&activeICCID, &activeName,
		); err != nil {
			return nil, err
		}
		if notes.Valid {
			v := notes.String
			c.Notes = &v
		}
		if firmware.Valid {
			v := firmware.String
			c.EUICCFirmware = &v
		}
		if pver.Valid {
			v := pver.String
			c.ProfileVersion = &v
		}
		if freeNVM.Valid {
			v := freeNVM.Int64
			c.FreeNVM = &v
		}
		if modemID.Valid {
			v := modemID.Int64
			c.ModemID = &v
		}
		if lastSeen.Valid {
			v := lastSeen.String
			c.LastSeenAt = &v
		}
		if modemDev.Valid {
			v := modemDev.String
			c.ModemDeviceID = &v
		}
		if modemModel.Valid {
			v := modemModel.String
			c.ModemModel = &v
		}
		if qmi.Valid && qmi.String != "" {
			t := "qmi"
			c.Transport = &t
		} else if mbim.Valid && mbim.String != "" {
			t := "mbim"
			c.Transport = &t
		}
		if activeICCID.Valid {
			v := activeICCID.String
			c.ActiveICCID = &v
		}
		if activeName.Valid && activeName.String != "" {
			v := activeName.String
			c.ActiveName = &v
		}
		c.VendorDisplay = vendorDisplay(c.Vendor)
		out = append(out, c)
	}
	return out, rows.Err()
}

// queryProfiles 列出 cardID 下的所有 profile。
func (s *Service) queryProfiles(ctx context.Context, cardID int64) ([]ESimProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, card_id, iccid, isdp_aid, state, nickname,
       service_provider, profile_name, profile_class, last_refreshed_at
FROM esim_profiles
WHERE card_id = ?
ORDER BY (state = 'enabled') DESC, iccid`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ESimProfile{}
	for rows.Next() {
		var p ESimProfile
		if err := rows.Scan(&p.ID, &p.CardID, &p.ICCID, &p.ISDPAid, &p.State,
			&p.Nickname, &p.ServiceProvider, &p.ProfileName, &p.ProfileClass,
			&p.LastRefreshedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// cardByID 单 card 查询（不带 join）。不存在返回 ErrCardNotFound。
func (s *Service) cardByID(ctx context.Context, id int64) (ESimCard, error) {
	cards, err := s.queryCards(ctx, id)
	if err != nil {
		return ESimCard{}, err
	}
	if len(cards) == 0 {
		return ESimCard{}, ErrCardNotFound
	}
	return cards[0], nil
}

// profileByICCID 查 profile 行；不存在返回 ErrProfileNotFound。
func (s *Service) profileByICCID(ctx context.Context, iccid string) (ESimProfile, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, card_id, iccid, isdp_aid, state, nickname,
       service_provider, profile_name, profile_class, last_refreshed_at
FROM esim_profiles WHERE iccid = ?`, iccid)
	var p ESimProfile
	if err := row.Scan(&p.ID, &p.CardID, &p.ICCID, &p.ISDPAid, &p.State,
		&p.Nickname, &p.ServiceProvider, &p.ProfileName, &p.ProfileClass,
		&p.LastRefreshedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrProfileNotFound
		}
		return p, err
	}
	return p, nil
}

// ---------------- helpers ----------------

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableI64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func profileDisplayName(p ESimProfile) string {
	for _, v := range []string{derefStr(p.Nickname), derefStr(p.ServiceProvider), derefStr(p.ProfileName), p.ICCID} {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return p.ICCID
}
