import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ThemeToggle } from "./theme-toggle";

describe("主题选择", () => {
  it("读取持久化选择并在 light/dark/system 间同步根节点", async () => {
    window.localStorage.setItem("reposentinel-theme", "dark");
    const user = userEvent.setup();
    render(<ThemeToggle />);

    const select = screen.getByRole("combobox", { name: "主题" });
    expect(select).toHaveValue("dark");
    expect(document.documentElement).toHaveClass("dark");

    await user.selectOptions(select, "light");
    expect(window.localStorage.getItem("reposentinel-theme")).toBe("light");
    expect(document.documentElement).not.toHaveClass("dark");

    vi.spyOn(window, "matchMedia").mockReturnValue({
      matches: true,
      media: "(prefers-color-scheme: dark)",
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    });
    await user.selectOptions(select, "system");

    expect(window.localStorage.getItem("reposentinel-theme")).toBe("system");
    expect(document.documentElement).toHaveClass("dark");
  });

  it("切换主题时同步更新 theme-color meta", async () => {
    const meta = document.createElement("meta");
    meta.name = "theme-color";
    document.head.appendChild(meta);
    try {
      window.localStorage.setItem("reposentinel-theme", "light");
      const user = userEvent.setup();
      render(<ThemeToggle />);

      expect(meta.content).toBe("#efe9df");

      const select = screen.getByRole("combobox", { name: "主题" });
      await user.selectOptions(select, "dark");
      expect(meta.content).toBe("#1e1510");

      await user.selectOptions(select, "light");
      expect(meta.content).toBe("#efe9df");
    } finally {
      meta.remove();
    }
  });
});
