import { useEffect, useRef, useState } from "react";
import { Activity, CheckCircle2, Clock, Pause, Play, XCircle } from "lucide-react";

import { subscribeDeliveryStage, type SSEEventPayload } from "../../../lib/sse-client";

export interface DeliveryStageInfo {
  stage: string;
  durationMs: number;
  detail?: string;
  occurredAt: string;
}

export interface LiveDeliveryItem {
  id: string;
  deliveryId: string;
  eventType?: string;
  repo?: string;
  stages: Record<string, DeliveryStageInfo>;
  currentStage: string;
  totalDurationMs: number;
  status: "processing" | "completed" | "failed";
  updatedAt: number;
}

const STAGE_ORDER = [
  { key: "accepted", label: "已接收", color: "bg-blue-500", trackColor: "border-blue-500/40 text-blue-400" },
  { key: "processing", label: "排队处理", color: "bg-amber-500", trackColor: "border-amber-500/40 text-amber-400" },
  { key: "rules_evaluated", label: "规则评估", color: "bg-purple-500", trackColor: "border-purple-500/40 text-purple-400" },
  { key: "outbox_queued", label: "写入队列", color: "bg-indigo-500", trackColor: "border-indigo-500/40 text-indigo-400" },
  { key: "channel_delivered", label: "渠道投递", color: "bg-emerald-500", trackColor: "border-emerald-500/40 text-emerald-400" },
];

export function LiveInspectorWaterfall({ maxItems = 8 }: { maxItems?: number }) {
  const [deliveries, setDeliveries] = useState<LiveDeliveryItem[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    return subscribeDeliveryStage((event: SSEEventPayload) => {
      if (!event.resource_id) return;
      const deliveryId = event.resource_id;
      const stage = event.stage || "processing";
      const durationMs = event.duration_ms ?? 0;
      const detail = event.detail;

      setDeliveries((prev) => {
        const existingIdx = prev.findIndex((d) => d.deliveryId === deliveryId);
        const now = Date.now();
        const stageInfo: DeliveryStageInfo = {
          stage,
          durationMs,
          detail,
          occurredAt: event.occurred_at,
        };

        const isCompleted = stage === "channel_delivered";
        const isFailed = detail?.startsWith("failed:") || false;

        if (existingIdx >= 0) {
          const item = prev[existingIdx];
          if (!item) return prev;
          const updatedStages = { ...item.stages, [stage]: stageInfo };
          const maxDur = Math.max(item.totalDurationMs, durationMs);
          const nextItem: LiveDeliveryItem = {
            ...item,
            stages: updatedStages,
            currentStage: stage,
            totalDurationMs: maxDur,
            status: isFailed ? "failed" : isCompleted ? "completed" : item.status,
            updatedAt: now,
          };
          const nextList = [...prev];
          nextList[existingIdx] = nextItem;
          return nextList;
        } else {
          const newItem: LiveDeliveryItem = {
            id: event.id || deliveryId,
            deliveryId,
            eventType: event.resource,
            stages: { [stage]: stageInfo },
            currentStage: stage,
            totalDurationMs: durationMs,
            status: isFailed ? "failed" : isCompleted ? "completed" : "processing",
            updatedAt: now,
          };
          return [newItem, ...prev].slice(0, maxItems);
        }
      });
    });
  }, [maxItems]);

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = 0;
    }
  }, [deliveries, autoScroll]);

  return (
    <div className="onboarding-card mb-6" data-testid="live-inspector-waterfall">
      <div className="flex items-center justify-between border-b border-border pb-3 mb-4">
        <div className="flex items-center gap-2">
          <Activity className="w-5 h-5 text-accent animate-pulse" />
          <h2 className="text-base font-semibold">Webhook 实时交付瀑布流 (Live Inspector)</h2>
          <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
            SSE 实时连线中
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="secondary-btn text-xs py-1 px-2.5 flex items-center gap-1.5"
            onClick={() => setAutoScroll((prev) => !prev)}
            aria-label={autoScroll ? "暂停自动滚动" : "开启自动滚动"}
          >
            {autoScroll ? (
              <>
                <Pause className="w-3.5 h-3.5" />
                暂停自动滚动
              </>
            ) : (
              <>
                <Play className="w-3.5 h-3.5" />
                开启自动滚动
              </>
            )}
          </button>
          {deliveries.length > 0 ? (
            <button
              type="button"
              className="secondary-btn text-xs py-1 px-2 text-muted-foreground hover:text-foreground"
              onClick={() => setDeliveries([])}
            >
              清空
            </button>
          ) : null}
        </div>
      </div>

      {deliveries.length === 0 ? (
        <div className="py-6 text-center text-sm text-muted-foreground">
          <Clock className="w-6 h-6 mx-auto mb-2 opacity-40 animate-spin" />
          <p>等待 GitHub Webhook 触发，实时交付阶段耗时瀑布流将在此呈现...</p>
        </div>
      ) : (
        <div ref={containerRef} className="space-y-3 max-h-72 overflow-y-auto pr-1">
          {deliveries.map((item) => (
            <div
              key={item.deliveryId}
              className="p-3 rounded-lg border border-border/60 bg-card/40 flex flex-col gap-2 hover:bg-card/70 transition-colors"
            >
              <div className="flex items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-foreground font-medium">
                    {item.deliveryId.length > 16 ? `${item.deliveryId.slice(0, 16)}...` : item.deliveryId}
                  </span>
                  <span className="text-muted-foreground">
                    耗时: <strong className="text-foreground">{item.totalDurationMs}ms</strong>
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  {item.status === "completed" ? (
                    <span className="inline-flex items-center gap-1 text-emerald-400">
                      <CheckCircle2 className="w-3.5 h-3.5" /> 已完成
                    </span>
                  ) : item.status === "failed" ? (
                    <span className="inline-flex items-center gap-1 text-rose-400">
                      <XCircle className="w-3.5 h-3.5" /> 失败
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-amber-400">
                      <Clock className="w-3.5 h-3.5 animate-spin" /> 处理中 ({item.currentStage})
                    </span>
                  )}
                </div>
              </div>

              {/* Waterfall visual track */}
              <div className="grid grid-cols-5 gap-1.5 pt-1">
                {STAGE_ORDER.map((stage) => {
                  const info = item.stages[stage.key];
                  const hasReached = Boolean(info);
                  return (
                    <div
                      key={stage.key}
                      className={`flex flex-col gap-1 p-1.5 rounded border text-[11px] transition-all ${
                        hasReached
                          ? stage.trackColor + " bg-card/60"
                          : "border-border/30 text-muted-foreground/40 bg-muted/20"
                      }`}
                      title={info ? `${stage.label}: ${info.durationMs}ms ${info.detail || ""}` : `${stage.label}: 等待中`}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{stage.label}</span>
                        {info ? <span>{info.durationMs}ms</span> : <span>-</span>}
                      </div>
                      <div className="w-full bg-muted/40 h-1 rounded-full overflow-hidden">
                        <div
                          className={`h-full transition-all duration-300 ${
                            hasReached ? stage.color : "bg-transparent"
                          }`}
                          style={{ width: hasReached ? "100%" : "0%" }}
                        />
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
