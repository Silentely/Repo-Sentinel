package updatecheck

import (
	"strconv"
	"strings"
)

// NormalizeVersion 去掉空白与可选 v/V 前缀。
func NormalizeVersion(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	return s
}

// ParseSemver 解析 major.minor.patch；忽略 prerelease/build；无法解析的段视为 0。
func ParseSemver(raw string) (major, minor, patch int) {
	s := NormalizeVersion(raw)
	if s == "" {
		return 0, 0, 0
	}
	s = strings.SplitN(s, "+", 2)[0]
	s = strings.SplitN(s, "-", 2)[0]
	parts := strings.Split(s, ".")
	out := make([]int, 0, 3)
	for _, piece := range parts {
		digits := ""
		for _, ch := range piece {
			if ch >= '0' && ch <= '9' {
				digits += string(ch)
			} else {
				break
			}
		}
		if digits == "" {
			out = append(out, 0)
		} else if n, err := strconv.Atoi(digits); err == nil {
			out = append(out, n)
		} else {
			out = append(out, 0)
		}
		if len(out) >= 3 {
			break
		}
	}
	for len(out) < 3 {
		out = append(out, 0)
	}
	return out[0], out[1], out[2]
}

// versionHasPrerelease 判定版本是否带预发布段（如 -rc1、-beta）。
func versionHasPrerelease(raw string) bool {
	s := NormalizeVersion(raw)
	s = strings.SplitN(s, "+", 2)[0]
	return strings.Contains(s, "-")
}

// compareVersions 按 SemVer 比较两版本：major/minor/patch 相等时，
// 无预发布段的一侧视为更新（正式版 > 预发布版，如 1.0.0 > 1.0.0-rc1）。
func compareVersions(a, b string) int {
	am, ai, ap := ParseSemver(a)
	bm, bi, bp := ParseSemver(b)
	if am != bm {
		if am > bm {
			return 1
		}
		return -1
	}
	if ai != bi {
		if ai > bi {
			return 1
		}
		return -1
	}
	if ap != bp {
		if ap > bp {
			return 1
		}
		return -1
	}
	aPre, bPre := versionHasPrerelease(a), versionHasPrerelease(b)
	if aPre != bPre {
		if aPre {
			return -1 // 带预发布段视为更旧
		}
		return 1
	}
	if aPre {
		// 两者都带预发布：按 SemVer 规则比较标识（数字段按数值，字母段按字典序，段更多优先）。
		return comparePrerelease(a, b)
	}
	return 0
}

// comparePrerelease 按 SemVer 规则比较两个预发布标识（入参为原始版本串）。
func comparePrerelease(a, b string) int {
	preA := strings.SplitN(a, "-", 2)
	preB := strings.SplitN(b, "-", 2)
	var as, bs []string
	if len(preA) == 2 {
		as = strings.Split(preA[1], ".")
	}
	if len(preB) == 2 {
		bs = strings.Split(preB[1], ".")
	}
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		} else {
			return -1 // 段更多优先（1.0.0-alpha.1 > 1.0.0-alpha）
		}
		if i < len(bs) {
			y = bs[i]
		} else {
			return 1
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1 // 数字段 < 字母段（1.0.0-alpha.1 < 1.0.0-alpha.a）
		case yerr == nil:
			return 1
		default:
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

// IsUpdateAvailable 当 latest 严格大于 current 时返回 true。
func IsUpdateAvailable(current, latest string) bool {
	cur := NormalizeVersion(current)
	lat := NormalizeVersion(latest)
	if cur == "" || lat == "" {
		return false
	}
	return compareVersions(cur, lat) < 0
}
