export interface TmaHaptics {
  impact: (style?: "light" | "medium" | "heavy") => void;
  notification: (type: "success" | "warning" | "error") => void;
  selection: () => void;
}

export const tmaHaptics: TmaHaptics = {
  impact(style: "light" | "medium" | "heavy" = "light"): void {
    if (typeof window === "undefined") {
      return;
    }
    try {
      window.Telegram?.WebApp?.HapticFeedback?.impactOccurred(style);
    } catch {
      // safe fallback
    }
  },
  notification(type: "success" | "warning" | "error"): void {
    if (typeof window === "undefined") {
      return;
    }
    try {
      window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred(type);
    } catch {
      // safe fallback
    }
  },
  selection(): void {
    if (typeof window === "undefined") {
      return;
    }
    try {
      window.Telegram?.WebApp?.HapticFeedback?.selectionChanged();
    } catch {
      // safe fallback
    }
  },
};

export function useTmaHaptics(): TmaHaptics {
  return tmaHaptics;
}

export function isTelegramWebAppAvailable(): boolean {
  return typeof window !== "undefined" && Boolean(window.Telegram?.WebApp?.initData);
}

/**
 * 将 Telegram 客户端传递的 themeParams 映射为 scoped CSS 变量，
 * 支持动态适配系统深色/浅色模式切换 (C3)。
 */
export function applyTmaTheme(): void {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return;
  }
  const theme = window.Telegram?.WebApp?.themeParams;
  if (!theme) return;

  const root = document.documentElement;
  if (theme.bg_color) root.style.setProperty("--tg-theme-bg-color", theme.bg_color);
  if (theme.text_color) root.style.setProperty("--tg-theme-text-color", theme.text_color);
  if (theme.hint_color) root.style.setProperty("--tg-theme-hint-color", theme.hint_color);
  if (theme.link_color) root.style.setProperty("--tg-theme-link-color", theme.link_color);
  if (theme.button_color) root.style.setProperty("--tg-theme-button-color", theme.button_color);
  if (theme.button_text_color) root.style.setProperty("--tg-theme-button-text-color", theme.button_text_color);
  if (theme.secondary_bg_color) root.style.setProperty("--tg-theme-secondary-bg-color", theme.secondary_bg_color);
}

/**
 * 初始化 TMA SDK：
 * 1. 展开视图并通知客户端准备就绪；
 * 2. 注入视口与主题参数；
 * 3. 监听 themeChanged 事件，返回销毁清理回调 (C2 & C3)。
 */
export function initTmaSdk(): () => void {
  if (typeof window === "undefined") {
    return () => {};
  }
  const webApp = window.Telegram?.WebApp;
  if (webApp) {
    try {
      webApp.ready?.();
      webApp.expand?.();
    } catch {
      // Safe fallback: no-op if call fails
    }
  }

  applyTmaTheme();
  const onThemeChanged = () => {
    applyTmaTheme();
  };

  try {
    webApp?.onEvent?.("themeChanged", onThemeChanged);
  } catch {
    // safe fallback
  }

  if (typeof document !== "undefined") {
    let meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]');
    if (!meta) {
      meta = document.createElement("meta");
      meta.name = "viewport";
      meta.content = "width=device-width, initial-scale=1.0, viewport-fit=cover";
      document.head.appendChild(meta);
    }
  }

  return () => {
    try {
      webApp?.offEvent?.("themeChanged", onThemeChanged);
    } catch {
      // safe fallback
    }
  };
}

export function getTmaStartParam(): string | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  return window.Telegram?.WebApp?.initDataUnsafe?.start_param;
}
