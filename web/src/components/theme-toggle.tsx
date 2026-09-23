import { Monitor, Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

export type ThemeMode = "light" | "dark" | "system";

const themeStorageKey = "reposentinel-theme";

// 主题背景色：运行时从设计令牌（tokens.css 的 --bg-canvas）读取，令牌即单一来源；
// CSS 未就绪（首帧脚本阶段/测试环境）时回退到与令牌等价的 hex。
// index.html 与 public/theme-init.js 的首帧预置仍内联 hex（CSS 解析前无法读取变量）。
function canvasColor(dark: boolean): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue("--bg-canvas").trim();
  if (value) return value;
  return dark ? "#1e1510" : "#efe9df";
}

export function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>(readStoredTheme);

  useEffect(() => {
    window.localStorage.setItem(themeStorageKey, mode);
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const dark = mode === "dark" || (mode === "system" && media.matches);
      document.documentElement.classList.toggle("dark", dark);
      // 切换主题后同步更新浏览器 UI 颜色；首帧取值由 public/theme-init.js 负责。
      const meta = document.querySelector('meta[name="theme-color"]');
      if (meta) {
        meta.setAttribute("content", canvasColor(dark));
      }
    };
    apply();
    if (mode === "system") {
      media.addEventListener("change", apply);
      return () => media.removeEventListener("change", apply);
    }
    return undefined;
  }, [mode]);

  const Icon = mode === "light" ? Sun : mode === "dark" ? Moon : Monitor;

  return (
    <label className="theme-toggle">
      <Icon aria-hidden="true" size={16} strokeWidth={1.8} />
      <span className="sr-only">主题</span>
      <select
        aria-label="主题"
        value={mode}
        onChange={(event) => setMode(parseThemeMode(event.target.value))}
      >
        <option value="system">跟随系统</option>
        <option value="light">浅色</option>
        <option value="dark">深色</option>
      </select>
    </label>
  );
}

// select 的 option 虽是字面量，DOM 类型仍是 string：收敛函数让新增选项时的类型检查仍然生效
// （断言会把任何字符串直接当 ThemeMode，绕过编译期校验）。
function parseThemeMode(value: string): ThemeMode {
  return value === "light" || value === "dark" || value === "system" ? value : "system";
}

function readStoredTheme(): ThemeMode {
  return parseThemeMode(window.localStorage.getItem(themeStorageKey) ?? "");
}
