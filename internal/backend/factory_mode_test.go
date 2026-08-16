package backend

import "testing"

// ValidateBackendMode 曾经问的是 NormalizeBackendMode 的结果，而归一化把认不出的值
// 全归成 AT——于是它结构上不可能返回非 nil。`auto` 这类值一路"通过"校验，再静默
// 变成 AT 模式。下面这张表里的非法值就是那次回归的判据。
func TestValidateBackendModeRejectsUnknown(t *testing.T) {
	valid := []string{"", "at", "qmi", "mbim", "AT", " QMI ", "MBIM"}
	for _, in := range valid {
		if err := ValidateBackendMode(in); err != nil {
			t.Errorf("ValidateBackendMode(%q) 应通过，得到 %v", in, err)
		}
	}

	invalid := []string{
		"auto",  // 曾写在 config.go 的字段注释里，实际从未实现
		"pcsc",  // 读卡器不经 NewBackend，不是本函数的取值域
		"qmii",  // 拼错
		"rndis", // 数据面是 QMI，不收 RNDIS
		"none",
	}
	for _, in := range invalid {
		if err := ValidateBackendMode(in); err == nil {
			t.Errorf("ValidateBackendMode(%q) 应被拒绝，却通过了", in)
		}
	}
}

// 归一化仍要兜底：配置已经落盘，运行期报错没人接，降级跑总比起不来强。
// 这与上面的严格校验不矛盾——拒绝发生在写入之前，兜底发生在写入之后。
func TestNormalizeBackendModeFallsBackToAT(t *testing.T) {
	cases := map[string]string{
		"":      BackendAT,
		"at":    BackendAT,
		"AT":    BackendAT,
		" qmi ": BackendQMI,
		"MBIM":  BackendMBIM,
		"auto":  BackendAT,
		"qmii":  BackendAT,
	}
	for in, want := range cases {
		if got := NormalizeBackendMode(in); got != want {
			t.Errorf("NormalizeBackendMode(%q) = %q，想要 %q", in, got, want)
		}
	}
}
