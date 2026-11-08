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

export function isTelegramWebAppAvailable(): boolean {
  return typeof window !== "undefined" && Boolean(window.Telegram?.WebApp?.initData);
}

export function initTmaSdk(): void {
  if (typeof window === "undefined") {
    return;
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

  if (typeof document !== "undefined") {
    let meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]');
    if (!meta) {
      meta = document.createElement("meta");
      meta.name = "viewport";
      meta.content = "width=device-width, initial-scale=1.0, viewport-fit=cover";
      document.head.appendChild(meta);
    }
  }
}

export function getTmaStartParam(): string | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  return window.Telegram?.WebApp?.initDataUnsafe?.start_param;
}

export function useTmaHaptics(): TmaHaptics {
  return tmaHaptics;
}
