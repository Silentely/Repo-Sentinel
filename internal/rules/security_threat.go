package rules

import (
	"fmt"
	"strings"
)

// ThreatClassification 威胁情报分类结果
type ThreatClassification struct {
	CWEID       string   `json:"cwe_id"`
	OWASPCategory string `json:"owasp_category"`
	SeverityTier  string `json:"severity_tier"` // 🔴 CRITICAL, 🟠 HIGH, 🟡 MEDIUM, 🔵 LOW
	Remediation   string `json:"remediation"`
	Tags          []string `json:"tags"`
}

// cweMap 存储常见关键 CWE 到 OWASP 分类的映射
var cweMap = map[string]ThreatClassification{
	"CWE-89": {
		CWEID:         "CWE-89",
		OWASPCategory: "A03:2021-Injection (SQL Injection)",
		SeverityTier:  "🔴 CRITICAL",
		Remediation:   "使用参数化查询/预编译语句，杜绝动态拼接 SQL",
		Tags:          []string{"SQLi", "OWASP-A03", "Database"},
	},
	"CWE-79": {
		CWEID:         "CWE-79",
		OWASPCategory: "A03:2021-Injection (Cross-site Scripting)",
		SeverityTier:  "🟠 HIGH",
		Remediation:   "对不受信任的输入进行 HTML 实体编码或使用 DOMPurify/CSP 防护",
		Tags:          []string{"XSS", "OWASP-A03", "Frontend"},
	},
	"CWE-798": {
		CWEID:         "CWE-798",
		OWASPCategory: "A07:2021-Identification and Authentication Failures",
		SeverityTier:  "🔴 CRITICAL",
		Remediation:   "立即撤销并轮换硬编码凭据，迁移至环境变量或密钥管理器",
		Tags:          []string{"Hardcoded-Secret", "OWASP-A07", "Credentials"},
	},
	"CWE-22": {
		CWEID:         "CWE-22",
		OWASPCategory: "A01:2021-Broken Access Control (Path Traversal)",
		SeverityTier:  "🟠 HIGH",
		Remediation:   "校验文件路径规范化白名单，杜绝 ../ 越权路径遍历",
		Tags:          []string{"Path-Traversal", "OWASP-A01", "Filesystem"},
	},
	"CWE-502": {
		CWEID:         "CWE-502",
		OWASPCategory: "A08:2021-Software and Data Integrity Failures",
		SeverityTier:  "🔴 CRITICAL",
		Remediation:   "避免对未签名对象直接反序列化，采用安全序列化格式 (如 JSON/Protobuf)",
		Tags:          []string{"Deserialization", "OWASP-A08", "RCE"},
	},
}

// ClassifySecurityThreat 根据 CWE 编号或安全警报标题提取威胁情报
func ClassifySecurityThreat(cweOrTitle string) ThreatClassification {
	normalized := strings.ToUpper(strings.TrimSpace(cweOrTitle))

	// 精确匹配 CWE
	for cwe, info := range cweMap {
		if strings.Contains(normalized, cwe) {
			return info
		}
	}

	// 关键字模糊匹配
	if strings.Contains(normalized, "SQL") || strings.Contains(normalized, "INJECTION") {
		return cweMap["CWE-89"]
	}
	if strings.Contains(normalized, "XSS") || strings.Contains(normalized, "CROSS-SITE SCRIPT") {
		return cweMap["CWE-79"]
	}
	if strings.Contains(normalized, "SECRET") || strings.Contains(normalized, "TOKEN") || strings.Contains(normalized, "API KEY") {
		return cweMap["CWE-798"]
	}
	if strings.Contains(normalized, "TRAVERSAL") || strings.Contains(normalized, "DIRECTORY") {
		return cweMap["CWE-22"]
	}

	return ThreatClassification{
		CWEID:         "CWE-OTHER",
		OWASPCategory: "A10:2021-General Vulnerability",
		SeverityTier:  "🟡 MEDIUM",
		Remediation:   "依照官方安全通告指引核查并更新受影响依赖版本",
		Tags:          []string{"General-Security"},
	}
}

// FormatThreatCardSummary 格式化输出威胁概览摘要行
func (tc ThreatClassification) FormatThreatCardSummary() string {
	return fmt.Sprintf("%s [%s] %s | 建议: %s", tc.SeverityTier, tc.CWEID, tc.OWASPCategory, tc.Remediation)
}
