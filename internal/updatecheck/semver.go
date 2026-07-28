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

// IsUpdateAvailable 当 latest 严格大于 current 时返回 true。
func IsUpdateAvailable(current, latest string) bool {
	cur := NormalizeVersion(current)
	lat := NormalizeVersion(latest)
	if cur == "" || lat == "" {
		return false
	}
	cMaj, cMin, cPat := ParseSemver(cur)
	lMaj, lMin, lPat := ParseSemver(lat)
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}
