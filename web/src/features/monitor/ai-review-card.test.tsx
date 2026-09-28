import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AIReviewCard } from "./list-pages";
import type { CodeReviewResult, TriggerAIReviewReceipt } from "./api";

const review: CodeReviewResult = {
  summary: "变更结构清晰",
  score: 88,
  security_risks: [],
  breaking_risks: [],
  code_smells: [],
  reviewed_at: "2026-09-01T00:00:00Z",
  commented_on_pr: false,
  diff_truncated: false,
  head_sha: "sha-1",
};

const receipt: TriggerAIReviewReceipt = { status: "queued", work_item_id: "wi-1", head_sha: "sha-1" };

const { fetchMock, triggerMock } = vi.hoisted(() => ({
  fetchMock: vi.fn(),
  triggerMock: vi.fn(),
}));

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    fetchWorkItemAIReview: fetchMock,
    triggerWorkItemAIReview: triggerMock,
  };
});

function renderCard() {
  return render(<AIReviewCard workItemId="wi-1" />);
}

describe("AIReviewCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // jsdom 未实现剪贴板：打桩 writeText，否则复制分支走 catch、不会进入「已复制」态。
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    fetchMock.mockReset();
    triggerMock.mockReset();
    fetchMock.mockResolvedValue(null);
    triggerMock.mockResolvedValue(receipt);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("卸载后停止审查轮询", async () => {
    // 审查始终未落定：轮询循环会一直拉到 90s 上限。
    fetchMock.mockResolvedValue(null);
    const view = renderCard();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI 代码审查报告/ }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /立即发起 AI 审查/ }));
      await vi.advanceTimersByTimeAsync(1_000);
    });
    const callsAtUnmount = fetchMock.mock.calls.length;
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fetchMock.mock.calls.length).toBe(callsAtUnmount);
  });

  it("卸载后不复位复制提示定时器", async () => {
    fetchMock.mockResolvedValue({ ...review, reviewed_at: "2026-09-02T00:00:00Z" });
    const view = renderCard();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI 代码审查报告/ }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /复制报告/ }));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByRole("button", { name: /已复制/ })).toBeInTheDocument();
    // 卸载即清除复位定时器：无此清理时悬挂定时器会在卸载后写已卸载组件状态。
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
  });

  it("审查结论三类清单均按条目渲染且同文本不撞键", async () => {
    // 条目文本在结果里可能重复：key 仅用文本会撞键导致列表渲染错乱。
    fetchMock.mockResolvedValue({
      ...review,
      reviewed_at: "2026-09-04T00:00:00Z",
      security_risks: ["硬编码凭据", "硬编码凭据"],
      breaking_risks: ["移除公开接口"],
      code_smells: ["函数过长"],
    });
    const view = renderCard();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI 代码审查报告/ }));
    });
    expect(screen.getByText(/安全风险:/)).toBeInTheDocument();
    expect(screen.getByText(/破坏性兼容风险:/)).toBeInTheDocument();
    expect(screen.getByText(/优化建议:/)).toBeInTheDocument();
    expect(screen.getAllByText("硬编码凭据")).toHaveLength(2);
    view.unmount();
  });

  it("审查落定后展示结果并自动展开", async () => {
    fetchMock.mockResolvedValue(null);
    triggerMock.mockResolvedValue(receipt);
    const view = renderCard();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI 代码审查报告/ }));
    });
    // 首次轮询返回旧报告（reviewed_at 早于快照），第二次返回本次审查结果。
    fetchMock
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({ ...review, reviewed_at: "2026-09-03T00:00:00Z" });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /立即发起 AI 审查/ }));
      await vi.advanceTimersByTimeAsync(3_000);
    });
    expect(screen.getByText("88 分")).toBeInTheDocument();
    expect(screen.getByText("变更结构清晰")).toBeInTheDocument();
    view.unmount();
  });

  it("渲染置信度、分类、合并风险徽章、缺失测试与重构建议代码块", async () => {
    fetchMock.mockResolvedValue({
      ...review,
      reviewed_at: "2026-09-05T00:00:00Z",
      confidence: 4,
      category: "Security Fix",
      merge_risk: "Low",
      missing_tests: ["缺少嵌套原型污染 400 回归测试"],
      suggestions: [
        {
          title: "改用白名单校验",
          file_path: "proxy.ts",
          description: "使用 Object.keys 校验白名单属性",
          suggested_code: "const ALLOWED = ['a', 'b'];",
        },
      ],
    });
    const view = renderCard();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI 代码审查报告/ }));
    });
    expect(screen.getByText(/🎯 置信度 4\/5/)).toBeInTheDocument();
    expect(screen.getByText(/🏷️ Security Fix/)).toBeInTheDocument();
    expect(screen.getByText(/合并风险: Low/)).toBeInTheDocument();
    expect(screen.getByText(/缺失测试用例与回归风险:/)).toBeInTheDocument();
    expect(screen.getByText("缺少嵌套原型污染 400 回归测试")).toBeInTheDocument();
    expect(screen.getByText(/建议 1: 改用白名单校验/)).toBeInTheDocument();
    expect(screen.getByText("(proxy.ts)")).toBeInTheDocument();
    expect(screen.getByText("使用 Object.keys 校验白名单属性")).toBeInTheDocument();
    expect(screen.getByText("const ALLOWED = ['a', 'b'];")).toBeInTheDocument();

    // 验证复制报告包含数据表格与重构建议
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /复制报告/ }));
      await vi.advanceTimersByTimeAsync(0);
    });
    const writeTextMock = navigator.clipboard.writeText as unknown as { mock: { calls: string[][] } };
    const copiedText = writeTextMock.mock.calls[writeTextMock.mock.calls.length - 1][0];
    expect(copiedText).toContain("| 代码健康评分 | 审查置信度 | 变更类型 | 合并风险 |");
    expect(copiedText).toContain("### 🛠️ 建议采纳与重构示范");
    expect(copiedText).toContain("const ALLOWED = ['a', 'b'];");

    view.unmount();
  });
});
