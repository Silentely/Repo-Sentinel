import React from "react";

export const INCIDENT_TAG_OPTIONS = [
  { value: "security", label: "安全告警 (security)", desc: "安全漏洞、凭据泄露与高危风险" },
  { value: "ci", label: "工作流/CI (ci)", desc: "构建发布、主干集成与关键交付流失败" },
  { value: "dependency", label: "依赖升级 (dependency)", desc: "第三方依赖库、组件版本变动" },
  { value: "maintenance", label: "维护/发布 (maintenance)", desc: "基础设施、Release 发布与运维变更" },
  { value: "general", label: "日常杂项 (general)", desc: "文档、琐碎重构或未归类日常事务" },
] as const;

export interface IncidentTagSelectProps {
  value?: string[] | null;
  onChange: (tags: string[]) => void;
  routingEnabled?: boolean;
  disabled?: boolean;
}

export function IncidentTagSelect({
  value,
  onChange,
  routingEnabled = true,
  disabled = false,
}: IncidentTagSelectProps) {
  // 老旧渠道数据反序列化后 incident_tags 可能为 undefined 或 null，统一通过 !value || value.length === 0 判定全接收
  const safeTags = Array.isArray(value) ? value : [];
  const isAll = safeTags.length === 0;

  const toggleTag = (tag: string) => {
    if (safeTags.includes(tag)) {
      onChange(safeTags.filter((t) => t !== tag));
    } else {
      onChange([...safeTags, tag]);
    }
  };

  const handleSelectAll = () => {
    // 空数组表示接收全部标签
    onChange([]);
  };


  return (
    <div className="incident-tag-select" style={{ marginTop: "0.5rem" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: "0.5rem", marginBottom: "0.25rem" }}>
        <span style={{ fontWeight: 600, fontSize: "0.9rem" }}>事件分类过滤（智能路由标签）</span>
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={handleSelectAll}
            disabled={disabled}
            style={{ fontSize: "0.75rem" }}
          >
            全部接收
          </button>
          <span className="muted" style={{ fontSize: "0.75rem" }}>
            {isAll ? "接收全部标签" : `已选 ${safeTags.length} / ${INCIDENT_TAG_OPTIONS.length}`}
          </span>
        </div>
      </div>

      {!routingEnabled && (
        <p className="field-hint" style={{ color: "var(--warning-fg)", margin: "0.25rem 0 0.5rem" }}>
          ⚠️ Jev 智能路由当前未开启，暂不应用标签过滤（所有通知仍按既有规则全量投递）。
        </p>
      )}

      {isAll && routingEnabled && (
        <p className="field-hint" style={{ margin: "0.25rem 0 0.5rem" }}>
          💡 当前未指定过滤标签，将<strong>接收全部标签</strong>的事件通知。若只需特定分类（如仅关注安全告警），请勾选对应标签。
        </p>
      )}

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
          gap: "0.5rem",
          marginTop: "0.25rem",
        }}
      >
        {INCIDENT_TAG_OPTIONS.map((opt) => {
          const isChecked = safeTags.includes(opt.value);
          return (
            <label
              key={opt.value}
              className="check-row"
              style={{
                alignItems: "flex-start",
                padding: "0.35rem 0.5rem",
                borderRadius: "4px",
                border: "1px solid var(--border-subtle)",
                background: isChecked ? "var(--bg-panel-warm)" : "transparent",
                cursor: disabled ? "not-allowed" : "pointer",
              }}
            >
              <input
                type="checkbox"
                checked={isChecked}
                disabled={disabled}
                onChange={() => toggleTag(opt.value)}
                style={{ marginTop: "0.2rem" }}
              />
              <span style={{ display: "flex", flexDirection: "column" }}>
                <span style={{ fontSize: "0.85rem", fontWeight: 500 }}>{opt.label}</span>
                <span className="muted" style={{ fontSize: "0.75rem" }}>{opt.desc}</span>
              </span>
            </label>
          );
        })}
      </div>
    </div>
  );
}
