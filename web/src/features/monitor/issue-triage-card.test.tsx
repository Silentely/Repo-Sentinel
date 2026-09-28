import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IssueTriageCard } from "./list-pages";
import type { IssueTriageResult } from "./api";

const mockTriage: IssueTriageResult = {
  category: "Bug Report",
  priority: "P1 High",
  summary: "用户在登录页输入验证码后偶现 500 崩溃错误",
  missing_details: ["复现步骤", "控制台错误日志堆栈"],
  suggested_reply: "感谢反馈！我们正在排查该偶发异常，能否请您补充一下报错时的控制台日志堆栈与浏览器版本？",
  confidence: 4,
  triaged_at: "2026-09-28T12:00:00Z",
};

const { fetchTriageMock, triggerTriageMock } = vi.hoisted(() => ({
  fetchTriageMock: vi.fn(),
  triggerTriageMock: vi.fn(),
}));

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    fetchWorkItemAITriage: fetchTriageMock,
    triggerWorkItemAITriage: triggerTriageMock,
  };
});

function renderCard(itemHtmlUrl?: string) {
  return render(
    <IssueTriageCard
      workItemId="wi-issue-1"
      item={itemHtmlUrl ? ({ id: "wi-issue-1", html_url: itemHtmlUrl } as any) : undefined}
    />
  );
}

describe("IssueTriageCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    fetchTriageMock.mockReset();
    triggerTriageMock.mockReset();
    fetchTriageMock.mockResolvedValue(null);
    triggerTriageMock.mockResolvedValue({ status: "queued", work_item_id: "wi-issue-1" });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("无分诊结果时展开展示空态与立即分诊按钮", async () => {
    fetchTriageMock.mockResolvedValue(null);
    renderCard();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI Issue 分诊与首响应/ }));
    });

    expect(screen.getByText(/该 Issue 暂无 AI 智能分诊结果/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /立即分诊/ })).toBeInTheDocument();
  });

  it("展开已分诊 Issue 时展示优先级、诉求摘要、缺失要素与首响建议", async () => {
    fetchTriageMock.mockResolvedValue(mockTriage);
    renderCard("https://github.com/owner/repo/issues/1");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI Issue 分诊与首响应/ }));
    });

    expect(screen.getByText("P1 High")).toBeInTheDocument();
    expect(screen.getByText("Bug Report")).toBeInTheDocument();
    expect(screen.getByText(/用户在登录页输入验证码后偶现 500 崩溃错误/)).toBeInTheDocument();
    expect(screen.getByText(/缺失排查要素/)).toBeInTheDocument();
    expect(screen.getByText("复现步骤")).toBeInTheDocument();
    expect(screen.getByText("控制台错误日志堆栈")).toBeInTheDocument();
    expect(screen.getByText(/感谢反馈！我们正在排查该偶发异常/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /前往回复/ })).toHaveAttribute("href", "https://github.com/owner/repo/issues/1");
  });

  it("点击复制建议首响应写入剪贴板并给出成功提示", async () => {
    fetchTriageMock.mockResolvedValue(mockTriage);
    renderCard();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI Issue 分诊与首响应/ }));
    });

    const copyBtn = screen.getByRole("button", { name: /复制建议首响应/ });
    await act(async () => {
      fireEvent.click(copyBtn);
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(mockTriage.suggested_reply);
    expect(screen.getByRole("button", { name: /已复制回复/ })).toBeInTheDocument();

    // 2s 后复位
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.getByRole("button", { name: /复制建议首响应/ })).toBeInTheDocument();
  });

  it("点击重新分诊调用 trigger 并更新界面数据", async () => {
    fetchTriageMock.mockResolvedValue(null);
    renderCard();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /AI Issue 分诊与首响应/ }));
    });

    const triggerBtn = screen.getByRole("button", { name: /立即分诊/ });
    await act(async () => {
      fireEvent.click(triggerBtn);
      await vi.advanceTimersByTimeAsync(1000);
    });

    expect(triggerTriageMock).toHaveBeenCalledWith("wi-issue-1");
		fetchTriageMock.mockResolvedValue(mockTriage);
		await act(async () => {
			await vi.advanceTimersByTimeAsync(2000);
		});
    expect(screen.getByText("P1 High")).toBeInTheDocument();
    expect(screen.getByText(/用户在登录页输入验证码后偶现 500 崩溃错误/)).toBeInTheDocument();
  });
});
