package backend

import (
	"fmt"
	"strings"

	"github.com/yuanshuai1122/vodoge/internal/modem"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

// 后端模式常量
const (
	BackendAT   = "at"
	BackendQMI  = "qmi"
	BackendMBIM = "mbim"
)

// NormalizeBackendMode 标准化后端模式字符串。
//
// 认不出的值一律归成 AT。这是运行期的兜底：配置已经写进来了，此时报错也没人接，
// 起不来的设备比降级跑的设备更糟。**拒绝非法值是 ValidateBackendMode 的事**，
// 那一步在写入之前。
//
// 注意 pcsc 不在这里：读卡器不经 NewBackend，另有一条路径（pool_pcsc.go）。
func NormalizeBackendMode(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", BackendAT:
		return BackendAT // 默认 AT 模式
	case BackendQMI:
		return BackendQMI
	case BackendMBIM:
		return BackendMBIM
	default:
		return BackendAT
	}
}

// ValidateBackendMode 验证后端模式是否有效。
//
// 判据是**原始输入**，不是 NormalizeBackendMode 的结果。此前这里问的是归一化之后
// 的值，而归一化对任何认不出的值都返回 AT——于是这个函数结构上不可能返回非 nil，
// 校验是假的：`auto`、`qmii`、`Q M I` 全都"通过"，然后静默变成 AT 模式。
//
// 空串合法，表示"未指定，用默认"。
func ValidateBackendMode(in string) error {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", BackendAT, BackendQMI, BackendMBIM:
		return nil
	default:
		return fmt.Errorf("无效的 device_backend 值: %q (可选: at, qmi, mbim)", in)
	}
}

// NewBackend 根据配置模式创建对应后端实例的工厂方法
// mode: "at" | "qmi"
// controlPath: QMI 控制设备路径（qmi 模式必须）
// m: modem.Manager（at 模式必须）
// source: QMI Core 资源源（qmi 模式必须）
func NewBackend(mode, controlPath string, m *modem.Manager, source QMISource, mbimSource MBIMSource) (DeviceBackend, error) {
	mode = NormalizeBackendMode(mode)

	switch mode {
	case BackendAT:
		if m == nil {
			return nil, fmt.Errorf("AT 模式需要 modem.Manager")
		}
		logger.Info("[backend] 使用 AT 后端模式")
		return NewATBackend(m), nil

	case BackendQMI:
		b, err := NewQMIBackend(controlPath, source)
		if err != nil {
			return nil, fmt.Errorf("QMI 后端初始化失败: %w", err)
		}
		logger.Info("[backend] 使用 QMI 后端模式", "control_path", controlPath)
		return b, nil

	case BackendMBIM:
		if mbimSource == nil {
			return nil, fmt.Errorf("MBIM 模式需要 MBIMSource")
		}
		logger.Info("[backend] 使用 MBIM 后端模式", "control_path", controlPath)
		return NewMBIMBackend(controlPath, mbimSource), nil

	default:
		return nil, fmt.Errorf("不支持的后端模式: %s", mode)
	}
}
