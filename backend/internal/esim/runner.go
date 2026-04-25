package esim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// lpacRunner 把对 lpac 二进制的 exec 与 JSON 解析包成一个对象。
// 通过 commander 字段允许测试注入伪造执行器。
type lpacRunner struct {
	bin        string
	driversDir string
	timeout    time.Duration
	cmd        commander
}

// commander 是 exec.CommandContext 的最小依赖面，便于测试 stub。
type commander interface {
	run(ctx context.Context, name string, args []string, env []string) (stdout, stderr []byte, exitCode int, err error)
}

// realCommander 是默认实现，直接调用 exec.CommandContext。
type realCommander struct{}

func (realCommander) run(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
			// 进程跑起来了但退出非 0：清掉 err，让上层根据 exit 判断
			err = nil
		}
	}
	return stdout.Bytes(), stderr.Bytes(), exit, err
}

func newLPACRunner(bin, driversDir string, timeout time.Duration) *lpacRunner {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &lpacRunner{
		bin:        bin,
		driversDir: driversDir,
		timeout:    timeout,
		cmd:        realCommander{},
	}
}

// available 检查 lpac 二进制是否可执行。
func (r *lpacRunner) available() bool {
	if r.bin == "" {
		return false
	}
	st, err := os.Stat(r.bin)
	if err != nil {
		return false
	}
	if st.IsDir() {
		return false
	}
	if st.Mode().Perm()&0o111 == 0 {
		return false
	}
	return true
}

// envFor 根据 transport 构造 lpac 期望的环境变量。
//
// lpac 实测要求（树莓派 6A 现场）：
//   - LPAC_APDU=qmi/mbim 选驱动
//   - LPAC_APDU_QMI_DEVICE / LPAC_APDU_MBIM_DEVICE 指定 cdc-wdm 设备
//   - LIBEUICC_DRIVER_SEARCH_PATH 兜底，让驱动 .so 能被发现
//   - LD_LIBRARY_PATH 包含 driversDir，因为 libeuicc-driver-loader.so.2 / libeuicc.so.2 /
//     liblpac-utils.so 与 lpac 二进制不在系统 ld.so.cache 内，必须在运行时显式可达
func (r *lpacRunner) envFor(t transportInfo) []string {
	env := []string{
		"LPAC_APDU=" + t.Kind,
	}
	switch t.Kind {
	case "qmi":
		env = append(env, "LPAC_APDU_QMI_DEVICE="+t.Device)
	case "mbim":
		env = append(env, "LPAC_APDU_MBIM_DEVICE="+t.Device)
	}
	if r.driversDir != "" {
		env = append(env, "LIBEUICC_DRIVER_SEARCH_PATH="+r.driversDir)
		// 把 driversDir 追加到 LD_LIBRARY_PATH 而非覆盖，保留进程已有的库搜索路径
		existing := os.Getenv("LD_LIBRARY_PATH")
		if existing != "" {
			env = append(env, "LD_LIBRARY_PATH="+r.driversDir+":"+existing)
		} else {
			env = append(env, "LD_LIBRARY_PATH="+r.driversDir)
		}
	}
	return env
}

// lpacEnvelope 是 lpac stdout 每行 JSON 对象的统一壳。
//
// 实测样例（chip info）：
//
//	{"type":"lpa","payload":{"code":0,"message":"success","data":{...}}}
//
// 也可能是 progress 类（"type":"progress"），我们忽略中间帧只取最后一条 payload。
type lpacEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type lpacPayload struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// runJSON 执行一次 lpac 命令并解析最后一帧 payload。
// 成功时 data 是 payload.Data 原始 JSON；失败时返回 *LPACError 包装。
func (r *lpacRunner) runJSON(ctx context.Context, t transportInfo, args []string) (json.RawMessage, error) {
	if !r.available() {
		return nil, ErrLPACUnavailable
	}
	cctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	stdout, stderr, code, err := r.cmd.run(cctx, r.bin, args, r.envFor(t))
	if err != nil {
		// exec 自身失败（找不到、信号杀死等）
		return nil, &LPACError{
			ExitCode: code,
			Detail:   "exec: " + err.Error(),
			Stderr:   string(stderr),
		}
	}

	// lpac 输出可能多帧 JSON line-delimited；取 type=lpa 的最后一帧。
	var last *lpacPayload
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env lpacEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Type != "lpa" {
			continue
		}
		var pl lpacPayload
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			continue
		}
		last = &pl
	}
	if last == nil {
		// 没有任何可用帧 —— exit 码也得报上去
		return nil, &LPACError{
			ExitCode: code,
			Detail:   fmt.Sprintf("no lpa payload (exit=%d)", code),
			Stderr:   string(stderr),
		}
	}
	if last.Code != 0 || code != 0 {
		msg := last.Message
		if msg == "" {
			msg = fmt.Sprintf("code=%d", last.Code)
		}
		return nil, &LPACError{
			ExitCode: code,
			Detail:   msg,
			Stderr:   string(stderr),
		}
	}
	return last.Data, nil
}

// chipInfoData 是 chip info 的解析结果（宽松 schema）。
type chipInfoData struct {
	EID            string
	ProfileVersion string
	EUICCFirmware  string
	FreeNVM        int64
}

// parseChipInfo 从 chip info 的 data 抽取我们关心的字段。
//
// lpac 不同版本的 schema 在迭代，常见键名：
//
//	{
//	  "eidValue": "353930...",
//	  "EUICCInfo2": {"profileVersion":"2.2.2","sas_acreditation_number":"..."},
//	  "EuiccConfiguredAddresses": {...},
//	  ...
//	}
//
// 我们只要 EID（最关键），其余尽力而为。
func parseChipInfo(raw json.RawMessage) (chipInfoData, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return chipInfoData{}, fmt.Errorf("chip info json: %w", err)
	}
	out := chipInfoData{}
	if v, ok := top["eidValue"]; ok {
		_ = json.Unmarshal(v, &out.EID)
	}
	if out.EID == "" {
		// 兼容旧 lpac："EID" 大写
		if v, ok := top["EID"]; ok {
			_ = json.Unmarshal(v, &out.EID)
		}
	}
	out.EID = strings.TrimSpace(out.EID)

	// EUICCInfo2 子结构里挖 profileVersion
	if v, ok := top["EUICCInfo2"]; ok {
		var info2 map[string]json.RawMessage
		if err := json.Unmarshal(v, &info2); err == nil {
			if pv, ok := info2["profileVersion"]; ok {
				_ = json.Unmarshal(pv, &out.ProfileVersion)
			}
			if fv, ok := info2["euiccFirmwareVer"]; ok {
				_ = json.Unmarshal(fv, &out.EUICCFirmware)
			}
			if nv, ok := info2["extCardResource"]; ok {
				// extCardResource 的 freeNonVolatileMemory 字段。
				var sub map[string]json.RawMessage
				if err := json.Unmarshal(nv, &sub); err == nil {
					if f, ok := sub["freeNonVolatileMemory"]; ok {
						_ = json.Unmarshal(f, &out.FreeNVM)
					}
				}
			}
		}
	}
	return out, nil
}

// profileEntry 是 lpac profile list 中单条 profile 的解析结果。
type profileEntry struct {
	ICCID           string
	ISDPAid         string
	State           string // enabled / disabled
	Nickname        string
	ServiceProvider string
	ProfileName     string
	ProfileClass    string
}

// parseProfileList 把 profile list 的 data（JSON 数组）转为 []profileEntry。
//
// lpac 输出样例：
//
//	[
//	  {"iccid":"894...","isdpAid":"A0000005591010FFFFFFFF8900001100",
//	   "profileState":"enabled","profileNickname":"",
//	   "serviceProviderName":"giffgaff","profileName":"giffgaff",
//	   "profileClass":"operational"}
//	]
//
// 字段名按 SGP.22 标准在 lpac 里有时是 camelCase，有时是带下划线，宽松处理。
func parseProfileList(raw json.RawMessage) ([]profileEntry, error) {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("profile list json: %w", err)
	}
	out := make([]profileEntry, 0, len(arr))
	for _, m := range arr {
		var p profileEntry
		readStr := func(keys ...string) string {
			for _, k := range keys {
				if v, ok := m[k]; ok {
					var s string
					if err := json.Unmarshal(v, &s); err == nil && s != "" {
						return s
					}
				}
			}
			return ""
		}
		p.ICCID = readStr("iccid", "ICCID")
		p.ISDPAid = readStr("isdpAid", "isdp_aid", "isdpAID")
		st := strings.ToLower(readStr("profileState", "state"))
		switch st {
		case "enabled", "1", "true":
			p.State = ProfileStateEnabled
		default:
			p.State = ProfileStateDisabled
		}
		p.Nickname = readStr("profileNickname", "profile_nickname", "nickname")
		p.ServiceProvider = readStr("serviceProviderName", "service_provider_name", "serviceProvider")
		p.ProfileName = readStr("profileName", "profile_name")
		p.ProfileClass = readStr("profileClass", "profile_class")

		if p.ICCID == "" {
			// 没 ICCID 就不入库
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// chipInfoCmd / profileListCmd / profileEnableCmd / profileDisableCmd /
// profileNicknameCmd 返回 lpac CLI 参数。
//
// 注意：lpac CLI 的 subcommand 形式（实测 5ber/9eSIM）：
//
//	lpac chip info
//	lpac profile list
//	lpac profile enable <iccid>
//	lpac profile disable <iccid>
//	lpac profile nickname <iccid> <name>
func chipInfoCmd() []string             { return []string{"chip", "info"} }
func profileListCmd() []string          { return []string{"profile", "list"} }
func profileEnableCmd(iccid string) []string  { return []string{"profile", "enable", iccid} }
func profileDisableCmd(iccid string) []string { return []string{"profile", "disable", iccid} }
func profileNicknameCmd(iccid, nick string) []string {
	return []string{"profile", "nickname", iccid, nick}
}
