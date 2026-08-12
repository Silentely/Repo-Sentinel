import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NumberField } from "./number-field";

function setup(overrides: { value?: number; min?: number; max?: number; integer?: boolean } = {}) {
  const onChange = vi.fn();
  const props = { value: 800, onChange, ...overrides };
  render(<NumberField {...props} />);
  const input = screen.getByRole("spinbutton") as HTMLInputElement;
  return { input, onChange };
}

describe("NumberField", () => {
  it("允许逐位输入多位数（不再被即时钳到下限）", () => {
    const { input, onChange } = setup({ min: 100, max: 8000 });
    // 逐位输入 2000：旧实现键入「2」即被钳到 100，无法继续。
    fireEvent.change(input, { target: { value: "2" } });
    expect(input).toHaveValue(2);
    fireEvent.change(input, { target: { value: "20" } });
    expect(input).toHaveValue(20);
    fireEvent.change(input, { target: { value: "200" } });
    expect(input).toHaveValue(200);
    fireEvent.change(input, { target: { value: "2000" } });
    expect(input).toHaveValue(2000);
    expect(onChange).toHaveBeenLastCalledWith(2000);
  });

  it("失焦时钳到 [min, max]", () => {
    const { input, onChange } = setup({ min: 100, max: 8000 });
    fireEvent.change(input, { target: { value: "50" } });
    fireEvent.blur(input);
    expect(input).toHaveValue(100);
    expect(onChange).toHaveBeenLastCalledWith(100);

    fireEvent.change(input, { target: { value: "99999" } });
    fireEvent.blur(input);
    expect(input).toHaveValue(8000);
    expect(onChange).toHaveBeenLastCalledWith(8000);
  });

  it("清空后失焦回退到下限", () => {
    const { input, onChange } = setup({ min: 1, max: 3600 });
    fireEvent.change(input, { target: { value: "" } });
    expect(input).toHaveValue(null);
    fireEvent.blur(input);
    expect(input).toHaveValue(1);
    expect(onChange).toHaveBeenLastCalledWith(1);
  });

  it("整数模式失焦截断小数", () => {
    const { input, onChange } = setup({ integer: true, min: 0, max: 5 });
    fireEvent.change(input, { target: { value: "2.7" } });
    fireEvent.blur(input);
    expect(input).toHaveValue(2);
    expect(onChange).toHaveBeenLastCalledWith(2);
  });
});
