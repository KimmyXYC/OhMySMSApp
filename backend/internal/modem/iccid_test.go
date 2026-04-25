package modem

import "testing"

func TestNormalizeICCID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 现场观察的 9eSIM：19 位 + F padding。
		{"19-digit-with-F-padding", "894921007608614852F", "894921007608614852"},
		// 同样模式但小写 f。
		{"19-digit-with-lowercase-f", "894921007608614852f", "894921007608614852"},
		// 极端情况：20 位带 FF padding。
		{"20-digit-with-FF-padding", "8949210076086148520F", "8949210076086148520"},
		// 已规范化的 19 位 ICCID（末位是数字 Luhn 校验码）。
		{"19-digit-already-clean", "8944110069156835483", "8944110069156835483"},
		// 已规范化的 18 位 ICCID（不应被剥）。
		{"18-digit-already-clean", "894921007608614852", "894921007608614852"},
		// 18 位末位恰好是 F：保守保留（理论上 Luhn 校验码不会是 F，但更保守不动）。
		{"18-digit-ending-F-preserved", "89492100760861485F", "89492100760861485F"},
		// 中国电信 psim ICCID 19 位。
		{"china-telecom-psim", "8985203105011606981", "8985203105011606981"},
		// 带前后空白。
		{"trim-whitespace", "  894921007608614852F  ", "894921007608614852"},
		// 空串。
		{"empty", "", ""},
		// 只有空白。
		{"only-whitespace", "   ", ""},
		// 长度小于等于 18 的整体保留（不会进剥除分支）。
		{"short-input", "12345F", "12345F"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeICCID(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizeICCID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
