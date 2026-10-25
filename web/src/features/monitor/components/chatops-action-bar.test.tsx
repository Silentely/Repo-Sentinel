import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ChatOpsActionBar } from "./chatops-action-bar";
import * as api from "../api";

vi.mock("../api", () => ({
  executeChatOpsWebAction: vi.fn(),
}));

describe("ChatOpsActionBar", () => {
  it("renders suggested reply button and executes action on click", async () => {
    const mockExecute = vi.mocked(api.executeChatOpsWebAction).mockResolvedValue({
      status: "executed",
      message: "已回复草稿",
      action: "issue_reply_draft",
    });

    const onSuccess = vi.fn();
    render(
      <ChatOpsActionBar
        repoId="repo-123"
        issueNumber={42}
        suggestedReply="Thanks for reporting!"
        onSuccess={onSuccess}
      />
    );

    const replyBtn = screen.getByText("🚀 采纳发送首响应");
    expect(replyBtn).toBeDefined();

    fireEvent.click(replyBtn);

    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith({
        action: "issue_reply_draft",
        repo_id: "repo-123",
        issue_number: 42,
        run_id: undefined,
        payload: "Thanks for reporting!",
      });
      expect(onSuccess).toHaveBeenCalled();
      expect(screen.getByText("✓ 已回复草稿")).toBeDefined();
    });
  });

  it("renders PR approve button when isPR is true", async () => {
    const mockExecute = vi.mocked(api.executeChatOpsWebAction).mockResolvedValue({
      status: "executed",
      message: "审批通过",
      action: "pr_quick_approve",
    });

    render(<ChatOpsActionBar repoId="repo-123" issueNumber={10} isPR={true} />);

    const approveBtn = screen.getByText("👍 快速通过审批");
    fireEvent.click(approveBtn);

    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith(
        expect.objectContaining({ action: "pr_quick_approve" })
      );
    });
  });

  it("displays error message on execution failure", async () => {
    vi.mocked(api.executeChatOpsWebAction).mockRejectedValue(new Error("网络超时"));

    render(
      <ChatOpsActionBar
        repoId="repo-123"
        issueNumber={42}
        suggestedReply="Hello"
      />
    );

    fireEvent.click(screen.getByText("🚀 采纳发送首响应"));

    await waitFor(() => {
      expect(screen.getByText("⚠ 网络超时")).toBeDefined();
    });
  });
});
