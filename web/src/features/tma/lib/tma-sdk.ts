/**
 * TMA SDK 集成与移动端交互辅助函数。
 * 遵循 Telegram WebApp 原生语义与静默降级设计规范。
 */

export interface TmaHaptics {
  impact: (style?: "light" | "medium" | "heavy" | "rigid" | "soft") => void;
  notification: (type: "success" | "warning" | "error") => void;
  selection: () => void;
}

/**
 * 触发触觉反馈。
 * 严格遵从静默降级原则：非 Telegram 环境或客户端不支持时直接静默忽略，不抛异常。
 */
export const tmaHaptics: TmaHaptics = {
  impact: (style: "light" | "medium" | "heavy" | "rigid" | "soft" = "light") => {
    try {
      window.Telegram?.WebApp?.HapticFeedback?.impactOccurred(style);
    } catch {
      // Safe fallback
    }
  },
  notification: (type: "error" | "success" | "warning") => {
    try {
      window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred(type);
    } catch {
      // Safe fallback
    }
  },
  selection: () => {
    try {
      window.Telegram?.WebApp?.HapticFeedback?.selectionChanged();
    } catch {
      // Safe fallback
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
 * 将 Telegram 客户端传递的主题参数注入 CSS 自定义变量 (CSS custom properties)。
 * 供 Tailwind 与深色模式主题直接无缝吸附。
 */
export function applyTmaTheme(): void {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return;
  }
  const theme = window.Telegram?.WebApp?.themeParams;
  if (!theme) {
    return;
  }

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

/**
 * 绑定 Telegram 原生 BackButton：
 * 显示返回按钮并挂载点击回调，组件卸载时清理事件监听并隐藏返回按钮 (C2)。
 */
export function bindTmaBackButton(onBack: () => void): () => void {
  if (typeof window === "undefined") {
    return () => {};
  }
  const backButton = window.Telegram?.WebApp?.BackButton;
  if (!backButton) {
    return () => {};
  }

  try {
    backButton.onClick(onBack);
    backButton.show();
  } catch {
    // safe fallback
  }

  return () => {
    try {
      backButton.offClick(onBack);
      backButton.hide();
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
