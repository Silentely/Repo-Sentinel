import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { INCIDENT_TAG_OPTIONS, IncidentTagSelect } from "./incident-tag-select";

describe("IncidentTagSelect", () => {
  it("处理 undefined 与 null 不报错，并提示接收全部标签", () => {
    const onChange = vi.fn();
    const { rerender } = render(<IncidentTagSelect value={undefined} onChange={onChange} />);

    expect(screen.getAllByText("接收全部标签").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/当前未指定过滤标签/)).toBeInTheDocument();

    // 检查 5 个 checkbox 默认均未选中
    const checkboxes = screen.getAllByRole("checkbox");
    expect(checkboxes).toHaveLength(5);
    checkboxes.forEach((cb) => {
      expect(cb).not.toBeChecked();
    });

    rerender(<IncidentTagSelect value={null} onChange={onChange} />);
    expect(screen.getAllByText("接收全部标签").length).toBeGreaterThanOrEqual(1);
  });

  it("勾选单个标签后触发 onChange", () => {
    const onChange = vi.fn();
    render(<IncidentTagSelect value={[]} onChange={onChange} />);

    const securityCheckbox = screen.getByRole("checkbox", { name: /安全告警/ });
    fireEvent.click(securityCheckbox);
    expect(onChange).toHaveBeenCalledWith(["security"]);
  });

  it("取消已选中的标签触发 onChange", () => {
    const onChange = vi.fn();
    render(<IncidentTagSelect value={["security", "ci"]} onChange={onChange} />);

    expect(screen.getByText("已选 2 / 5")).toBeInTheDocument();

    const securityCheckbox = screen.getByRole("checkbox", { name: /安全告警/ });
    expect(securityCheckbox).toBeChecked();

    fireEvent.click(securityCheckbox);
    expect(onChange).toHaveBeenCalledWith(["ci"]);
  });

  it("点击「全部接收」按钮重置为空数组", () => {
    const onChange = vi.fn();
    render(<IncidentTagSelect value={["security", "maintenance"]} onChange={onChange} />);

    const allBtn = screen.getByRole("button", { name: "全部接收" });
    fireEvent.click(allBtn);
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it("当 routingEnabled 为 false 时提示暂不应用标签过滤", () => {
    const onChange = vi.fn();
    render(<IncidentTagSelect value={["security"]} onChange={onChange} routingEnabled={false} />);

    expect(screen.getByText(/Jev 智能路由当前未开启，暂不应用标签过滤/)).toBeInTheDocument();
  });
});
