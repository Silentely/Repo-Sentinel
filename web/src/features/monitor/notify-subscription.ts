// 渠道可订阅的实时通知类型（与后端 store.SubscribableEventKinds 保持一致）。
// featureKey 对应全局功能模块开关，用于 UI 灰显。
export const SUBSCRIBABLE_KINDS: Array<{
  value: string;
  label: string;
  featureKey: "feature.issues" | "feature.pull_requests" | "feature.actions" | "feature.security_alerts";
}> = [
  { value: "issue", label: "Issue", featureKey: "feature.issues" },
  { value: "pull_request", label: "PR", featureKey: "feature.pull_requests" },
  { value: "workflow_run", label: "Actions", featureKey: "feature.actions" },
  { value: "dependabot", label: "Dependabot", featureKey: "feature.security_alerts" },
  { value: "code_scanning", label: "Code Scanning", featureKey: "feature.security_alerts" },
  { value: "secret_scanning", label: "Secret Scanning", featureKey: "feature.security_alerts" },
];

// uiCheckedKinds 将后端的 event_kinds（null=订阅全部）映射为 UI 勾选态数组。
export function uiCheckedKinds(eventKinds: string[] | null): string[] {
  if (eventKinds === null) {
    return SUBSCRIBABLE_KINDS.map((k) => k.value);
  }
  return eventKinds;
}

// subscriptionSummary 用于渠道列表行的订阅摘要展示。
export function subscriptionSummary(eventKinds: string[] | null, digestEnabled: boolean): string {
  let kindsText: string;
  if (eventKinds === null || eventKinds.length === SUBSCRIBABLE_KINDS.length) {
    kindsText = "全部类型";
  } else if (eventKinds.length === 0) {
    kindsText = "不接收实时通知";
  } else {
    kindsText = SUBSCRIBABLE_KINDS.filter((k) => eventKinds.includes(k.value))
      .map((k) => k.label)
      .join("、");
  }
  return `订阅：${kindsText} · 定期汇总：${digestEnabled ? "开" : "关"}`;
}
