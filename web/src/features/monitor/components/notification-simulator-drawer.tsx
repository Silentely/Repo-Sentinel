import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, ChevronRight, Play, Sparkles, X, XCircle, AlertCircle } from "lucide-react";

import { useModalLayer } from "../../../lib/use-modal-layer";
import { dryRunRules, type DryRunParams, type DryRunResponse } from "../api";

const PRESET_PAYLOADS: Record<string, { action: string; repo: string; branch: string; payload: string }> = {
  pull_request: {
    action: "opened",
    repo: "octocat/Hello-World",
    branch: "main",
    payload: JSON.stringify(
      {
        action: "opened",
        repository: { full_name: "octocat/Hello-World", html_url: "https://github.com/octocat/Hello-World" },
        pull_request: {
          number: 101,
          title: "feat: add super fast notification simulator",
          html_url: "https://github.com/octocat/Hello-World/pull/101",
          user: { login: "octocat" },
          head: { ref: "feature/sim" },
          base: { ref: "main" },
        },
        sender: { login: "octocat" },
      },
      null,
      2
    ),
  },
  issues: {
    action: "opened",
    repo: "octocat/Hello-World",
    branch: "",
    payload: JSON.stringify(
      {
        action: "opened",
        repository: { full_name: "octocat/Hello-World", html_url: "https://github.com/octocat/Hello-World" },
        issue: {
          number: 42,
          title: "Bug: intermittent webhook timeout on large payload",
          body: "Steps to reproduce: push a commit with 50 files...",
          html_url: "https://github.com/octocat/Hello-World/issues/42",
          user: { login: "monalisa" },
        },
        sender: { login: "monalisa" },
      },
      null,
      2
    ),
  },
  workflow_run: {
    action: "completed",
    repo: "octocat/Hello-World",
    branch: "main",
    payload: JSON.stringify(
      {
        action: "completed",
        repository: { full_name: "octocat/Hello-World", html_url: "https://github.com/octocat/Hello-World" },
        workflow_run: {
          id: 987654321,
          name: "CI / Production Build",
          head_branch: "main",
          conclusion: "failure",
          html_url: "https://github.com/octocat/Hello-World/actions/runs/987654321",
        },
        sender: { login: "github-actions[bot]", type: "Bot" },
      },
      null,
      2
    ),
  },
};

export function NotificationSimulatorDrawer({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const panelRef = useModalLayer<HTMLElement>({ open, onClose });
  const [eventType, setEventType] = useState("pull_request");
  const [action, setAction] = useState(PRESET_PAYLOADS.pull_request.action);
  const [repo, setRepo] = useState(PRESET_PAYLOADS.pull_request.repo);
  const [branch, setBranch] = useState(PRESET_PAYLOADS.pull_request.branch);
  const [payloadRaw, setPayloadRaw] = useState(PRESET_PAYLOADS.pull_request.payload);

  const dryRunMutation = useMutation({
    mutationFn: (params: DryRunParams) => dryRunRules(params),
  });

  const handleApplyPreset = (type: string) => {
    setEventType(type);
    const preset = PRESET_PAYLOADS[type];
    if (preset) {
      setAction(preset.action);
      setRepo(preset.repo);
      setBranch(preset.branch);
      setPayloadRaw(preset.payload);
    }
  };

  const handleRun = () => {
    dryRunMutation.mutate({
      event_type: eventType,
      action,
      repository: repo,
      branch,
      payload_raw: payloadRaw,
    });
  };

  if (!open) return null;

  return (
    <div className="drawer-overlay" onClick={onClose} role="presentation">
      <aside
        ref={panelRef}
        className="drawer-panel max-w-2xl w-full"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="通知效果模拟器"
        tabIndex={-1}
      >
        <div className="drawer-panel__header flex items-center justify-between border-b border-border pb-3">
          <div className="flex items-center gap-2">
            <Sparkles className="w-5 h-5 text-accent" />
            <div>
              <h2 className="text-base font-semibold">通知效果多渠道模拟器</h2>
              <p className="text-xs text-muted-foreground">纯内存沙盒评估，不产生真实发送与数据库记录</p>
            </div>
          </div>
          <button className="quiet-button p-1 hover:text-foreground" type="button" onClick={onClose} aria-label="关闭">
            <X size={18} aria-hidden="true" />
          </button>
        </div>

        <div className="p-4 space-y-4 overflow-y-auto max-h-[calc(100vh-140px)]">
          {/* Preset selector */}
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs font-medium text-muted-foreground">预设模板:</span>
            {Object.keys(PRESET_PAYLOADS).map((k) => (
              <button
                key={k}
                type="button"
                className={`text-xs px-2.5 py-1 rounded border transition-colors ${
                  eventType === k
                    ? "border-accent bg-accent/10 text-accent font-medium"
                    : "border-border/60 hover:bg-muted/40 text-muted-foreground"
                }`}
                onClick={() => handleApplyPreset(k)}
              >
                {k}
              </button>
            ))}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground block mb-1">事件类型 (Event Type)</label>
              <input
                className="w-full text-xs font-mono p-2 rounded border border-border bg-background"
                value={eventType}
                onChange={(e) => setEventType(e.target.value)}
                placeholder="pull_request / issues / workflow_run"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground block mb-1">动作 (Action)</label>
              <input
                className="w-full text-xs font-mono p-2 rounded border border-border bg-background"
                value={action}
                onChange={(e) => setAction(e.target.value)}
                placeholder="opened / closed / completed"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground block mb-1">目标仓库 (Repository)</label>
              <input
                className="w-full text-xs font-mono p-2 rounded border border-border bg-background"
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                placeholder="org/repo"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground block mb-1">分支 (Branch, 可选)</label>
              <input
                className="w-full text-xs font-mono p-2 rounded border border-border bg-background"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="main / develop"
              />
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-muted-foreground">Webhook 载荷 JSON (Payload)</label>
              <span className="text-[11px] text-muted-foreground">将直接用于字段映射与提取</span>
            </div>
            <textarea
              className="w-full font-mono text-xs p-2.5 rounded border border-border bg-background h-32 resize-y"
              value={payloadRaw}
              onChange={(e) => setPayloadRaw(e.target.value)}
              placeholder="{ ... }"
            />
          </div>

          <div className="flex items-center justify-end gap-2 pt-1">
            <button
              type="button"
              className="primary-btn text-xs py-1.5 px-4 flex items-center gap-1.5"
              disabled={dryRunMutation.isPending}
              onClick={handleRun}
            >
              <Play className="w-3.5 h-3.5 fill-current" />
              {dryRunMutation.isPending ? "正在评估..." : "运行模拟评估 (Dry-Run)"}
            </button>
          </div>

          {dryRunMutation.isError ? (
            <div className="p-3 rounded border border-rose-500/30 bg-rose-500/10 text-rose-400 text-xs flex items-center gap-2">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>评估失败: {String(dryRunMutation.error)}</span>
            </div>
          ) : null}

          {dryRunMutation.data ? (
            <div className="space-y-4 pt-3 border-t border-border" data-testid="simulator-results">
              {/* Preview card */}
              <div>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                  消息卡片效果预览 (Rendered Preview)
                </h3>
                <div className="p-4 rounded-lg border border-border/80 bg-card/60 space-y-2">
                  <div className="text-sm font-semibold text-foreground flex items-center gap-1.5">
                    <span>{dryRunMutation.data.title}</span>
                  </div>
                  <div
                    className="text-xs text-muted-foreground whitespace-pre-wrap font-sans leading-relaxed border-t border-border/40 pt-2"
                    dangerouslySetInnerHTML={{ __html: dryRunMutation.data.body_text }}
                  />
                  {dryRunMutation.data.html_url ? (
                    <div className="text-[11px] text-accent pt-1">
                      <a href={dryRunMutation.data.html_url} target="_blank" rel="noreferrer" className="underline">
                        {dryRunMutation.data.html_url}
                      </a>
                    </div>
                  ) : null}
                </div>
              </div>

              {/* Channel routing matrix */}
              <div>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                  渠道路由与命中状态 (Channel Evaluation Matrix)
                </h3>
                {dryRunMutation.data.channel_results.length === 0 ? (
                  <p className="text-xs text-muted-foreground">系统中未配置任何通知渠道。</p>
                ) : (
                  <div className="space-y-2">
                    {dryRunMutation.data.channel_results.map((ch) => (
                      <div
                        key={ch.channel_id}
                        className="p-2.5 rounded border border-border/60 bg-card/30 flex items-center justify-between text-xs"
                      >
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-foreground">{ch.name || ch.channel_type}</span>
                          <span className="text-[11px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground uppercase">
                            {ch.channel_type}
                          </span>
                        </div>
                        <div className="flex items-center gap-1.5">
                          {ch.matched ? (
                            <span className="inline-flex items-center gap-1 text-emerald-400 font-medium">
                              <CheckCircle2 className="w-3.5 h-3.5" /> 命中投递
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-amber-400">
                              <XCircle className="w-3.5 h-3.5" /> 过滤 ({ch.reason || "未命中"})
                            </span>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Matched rules summary */}
              {dryRunMutation.data.matched_rules.length > 0 ? (
                <div>
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">
                    规则命中明细 (Matched Rules)
                  </h3>
                  <div className="flex flex-wrap gap-1.5">
                    {dryRunMutation.data.matched_rules.map((rule) => (
                      <span
                        key={rule}
                        className="text-[11px] px-2 py-0.5 rounded border border-indigo-500/20 bg-indigo-500/10 text-indigo-400 font-mono"
                      >
                        {rule}
                      </span>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      </aside>
    </div>
  );
}
