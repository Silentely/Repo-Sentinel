package textutil

import (
	htmlpkg "html"
	"regexp"
	"strings"
)

var rePlaceholder = regexp.MustCompile(`\{\{([a-zA-Z0-9_-]+)\}\}`)

// InterpolateSafeTemplate 对通知模板占位符进行安全插值替换。
func InterpolateSafeTemplate(tpl string, vars map[string]string, escapeHTML bool) string {
	if tpl == "" || len(vars) == 0 {
		return tpl
	}

	return rePlaceholder.ReplaceAllStringFunc(tpl, func(match string) string {
		key := strings.Trim(match, "{}")
		val, ok := vars[key]
		if !ok {
			return match
		}
		if escapeHTML {
			return htmlpkg.EscapeString(val)
		}
		return val
	})
}
