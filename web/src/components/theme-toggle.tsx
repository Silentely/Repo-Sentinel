import { Monitor, Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

export type ThemeMode = "light" | "dark" | "system";

const themeStorageKey = "reposentinel-theme";

// 与 index.html 内联预置脚本保持一致；色值取自 tokens.css 的 --bg-canvas。
const lightThemeColor = "#efe9df";
const darkThemeColor = "#1e1510";

export function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>(readStoredTheme);

  useEffect(() => {
    window.localStorage.setItem(themeStorageKey, mode);
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const dark = mode === "dark" || (mode === "system" && media.matches);
      document.documentElement.classList.toggle("dark", dark);
      // 切换主题后同步更新浏览器 UI 颜色；首帧取值由 index.html 内联脚本负责。
      const meta = document.querySelector('meta[name="theme-color"]');
      if (meta) {
        meta.setAttribute("content", dark ? darkThemeColor : lightThemeColor);
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
        onChange={(event) => setMode(event.target.value as ThemeMode)}
      >
        <option value="system">跟随系统</option>
        <option value="light">浅色</option>
        <option value="dark">深色</option>
      </select>
    </label>
  );
}

function readStoredTheme(): ThemeMode {
  const stored = window.localStorage.getItem(themeStorageKey);
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
}
