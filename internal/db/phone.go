// 号码的规范化与校验。
//
// 全是纯函数，不碰数据库。模组回上来的号码形态极杂：带 tel:/sip: 前缀、
// 带尖括号与引号、全 F（未写入）、全 0、甚至把 IMSI 当号码返回。
// 这些都必须在入库前挡掉，否则会以「本机号码」的身份显示给用户。
package db

import (
	"strings"
)

func normalizeSIMPhoneNumber(v string) string {
	s := canonicalLocalPhone(v)
	if s == "" {
		return ""
	}
	upper := strings.ToUpper(s)
	if upper == "FFFFFFFF" || upper == "FFFFFFFFFFFF" || upper == "00000000000" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(v), "Own Number") {
		return ""
	}
	if allSameRune(upper, 'F') || allSameRune(s, '0') {
		return ""
	}
	if !looksLikePhoneNumber(s) {
		return ""
	}
	return s
}

func allSameRune(s string, r rune) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch != r {
			return false
		}
	}
	return true
}

func canonicalLocalPhone(v string) string {
	s := strings.TrimSpace(v)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "tel:") {
		s = strings.TrimSpace(s[4:])
		lower = strings.ToLower(s)
	}
	if strings.HasPrefix(lower, "sip:") {
		s = strings.TrimSpace(s[4:])
		if idx := strings.IndexAny(s, "@;>"); idx >= 0 {
			s = s[:idx]
		}
	}
	s = strings.Trim(s, "<>\"")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "(", "")
	s = strings.ReplaceAll(s, ")", "")
	return strings.TrimSpace(s)
}

func looksLikePhoneNumber(v string) bool {
	s := canonicalLocalPhone(v)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if len(s) < 6 || len(s) > 15 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// phoneDigits 返回仅保留数字的形式（去掉前导 + 与分隔符）。
func phoneDigits(v string) string {
	s := canonicalLocalPhone(v)
	s = strings.TrimPrefix(s, "+")
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// phoneDigitsEqualIMSI 判断号码数字是否与 IMSI 数字完全相同（误学 IMSI 的特征）。
func phoneDigitsEqualIMSI(phone, imsi string) bool {
	pd := phoneDigits(phone)
	id := phoneDigits(imsi)
	return pd != "" && pd == id
}
