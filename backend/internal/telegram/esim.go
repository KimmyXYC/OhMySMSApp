package telegram

import (
	"fmt"
	"strings"
)

// cmdESim /esim 列出已发现的 eUICC 卡（MVP：只读）。
//
// 输出示例：
//
//   *eSIM 卡列表*
//
//   [#0] 5ber.eSIM
//   EID:   89086030...5376
//   Modem: EC20F (rb-9eSIM-A)
//   Active: giffgaff (894...5483)
//
// 风险性的 enable/disable 暂只走 Web UI。
func (b *bot) cmdESim() {
	if b.esim == nil {
		_, _ = b.sendText(escapeMarkdownV2("eSIM 模块未启用（缺少 lpac 配置）。"))
		return
	}
	cards, err := b.esim.ListCards(b.bgCtx())
	if err != nil {
		_, _ = b.sendText(escapeMarkdownV2("查询失败: " + err.Error()))
		return
	}
	if len(cards) == 0 {
		_, _ = b.sendText(escapeMarkdownV2(
			"还没有发现任何 eUICC。请确认贴片卡已插入并被 ModemManager 识别后，" +
				"在 Web UI 触发 Discover。"))
		return
	}

	var bld strings.Builder
	bld.WriteString("*eSIM 卡列表*\n")
	for i, c := range cards {
		bld.WriteString(escapeMarkdownV2(fmt.Sprintf("\n[#%d] ", i)))
		bld.WriteString("*")
		display := c.VendorDisplay
		if c.Nickname != nil && *c.Nickname != "" {
			display = *c.Nickname + " · " + display
		}
		bld.WriteString(escapeMarkdownV2(display))
		bld.WriteString("*\n")
		bld.WriteString(escapeMarkdownV2("EID:   …" + lastN(c.EID, 8)))
		bld.WriteString("\n")
		modemLabel := "(unbound)"
		if c.ModemModel != nil && *c.ModemModel != "" {
			modemLabel = *c.ModemModel
		} else if c.ModemDeviceID != nil {
			modemLabel = *c.ModemDeviceID
		}
		if c.Transport != nil {
			modemLabel += " [" + *c.Transport + "]"
		}
		bld.WriteString(escapeMarkdownV2("Modem: " + modemLabel))
		bld.WriteString("\n")
		if c.ActiveICCID != nil {
			act := "active: "
			if c.ActiveName != nil && *c.ActiveName != "" {
				act += *c.ActiveName + " (…" + lastN(*c.ActiveICCID, 6) + ")"
			} else {
				act += "…" + lastN(*c.ActiveICCID, 6)
			}
			bld.WriteString(escapeMarkdownV2(act))
			bld.WriteString("\n")
		} else {
			bld.WriteString(escapeMarkdownV2("active: (none)"))
			bld.WriteString("\n")
		}
	}
	bld.WriteString("\n")
	bld.WriteString(escapeMarkdownV2("启用/禁用 profile 请走 Web UI。"))
	_, _ = b.sendText(bld.String())
}
