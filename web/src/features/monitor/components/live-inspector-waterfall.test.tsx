import { fireEvent, render, screen } from "@testing-library/react";
import { act } from "react";
import { describe, expect, it } from "vitest";

import { LiveInspectorWaterfall } from "./live-inspector-waterfall";
import { emitDeliveryStage } from "../../../lib/sse-client";

describe("LiveInspectorWaterfall", () => {
  it("renders empty state initially and updates on incoming delivery stages", () => {
    render(<LiveInspectorWaterfall />);

    // Header exists
    expect(screen.getByText("Webhook 实时交付瀑布流 (Live Inspector)")).toBeInTheDocument();
    // Empty state message exists
    expect(
      screen.getByText("等待 GitHub Webhook 触发，实时交付阶段耗时瀑布流将在此呈现...")
    ).toBeInTheDocument();

    const deliveryId = "deliv-test-live-123456";

    // 1. Accepted stage
    act(() => {
      emitDeliveryStage({
        id: "evt-1",
        topic: "delivery.stage",
        version: 1,
        occurred_at: new Date().toISOString(),
        resource: "webhook_delivery",
        resource_id: deliveryId,
        stage: "accepted",
        duration_ms: 10,
        detail: "webhook accepted",
      });
    });

    expect(screen.getByText("deliv-test-live-...")).toBeInTheDocument();
    expect(screen.getByText("处理中 (accepted)")).toBeInTheDocument();

    // 2. Processing stage
    act(() => {
      emitDeliveryStage({
        id: "evt-2",
        topic: "delivery.stage",
        version: 1,
        occurred_at: new Date().toISOString(),
        resource: "webhook_delivery",
        resource_id: deliveryId,
        stage: "processing",
        duration_ms: 18,
        detail: "processing",
      });
    });

    // 3. Rules evaluated stage
    act(() => {
      emitDeliveryStage({
        id: "evt-3",
        topic: "delivery.stage",
        version: 1,
        occurred_at: new Date().toISOString(),
        resource: "webhook_delivery",
        resource_id: deliveryId,
        stage: "rules_evaluated",
        duration_ms: 45,
        detail: "rules evaluated",
      });
    });

    // 4. Outbox queued stage
    act(() => {
      emitDeliveryStage({
        id: "evt-4",
        topic: "delivery.stage",
        version: 1,
        occurred_at: new Date().toISOString(),
        resource: "webhook_delivery",
        resource_id: deliveryId,
        stage: "outbox_queued",
        duration_ms: 60,
        detail: "outbox queued",
      });
    });

    // 5. Channel delivered (completed) stage
    act(() => {
      emitDeliveryStage({
        id: "evt-5",
        topic: "delivery.stage",
        version: 1,
        occurred_at: new Date().toISOString(),
        resource: "webhook_delivery",
        resource_id: deliveryId,
        stage: "channel_delivered",
        duration_ms: 125,
        detail: "delivered",
      });
    });

    expect(screen.getByText("已完成")).toBeInTheDocument();
    expect(screen.getAllByText("125ms").length).toBeGreaterThanOrEqual(1);

    // Clear deliveries
    const clearBtn = screen.getByRole("button", { name: "清空" });
    fireEvent.click(clearBtn);

    expect(
      screen.getByText("等待 GitHub Webhook 触发，实时交付阶段耗时瀑布流将在此呈现...")
    ).toBeInTheDocument();
  });

  it("handles auto-scroll toggle", () => {
    render(<LiveInspectorWaterfall />);

    const scrollBtn = screen.getByRole("button", { name: "暂停自动滚动" });
    expect(scrollBtn).toBeInTheDocument();

    fireEvent.click(scrollBtn);
    expect(screen.getByRole("button", { name: "开启自动滚动" })).toBeInTheDocument();
  });
});
