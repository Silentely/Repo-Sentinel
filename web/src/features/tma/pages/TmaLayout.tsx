import { useEffect } from "react";
import { Outlet } from "@tanstack/react-router";
import { initTmaSdk, isTelegramWebAppAvailable } from "../lib/tma-sdk";

export function TmaLayout() {
  const isAvailable = isTelegramWebAppAvailable();

  useEffect(() => {
    if (isAvailable) {
      const cleanup = initTmaSdk();
      return cleanup;
    }
  }, [isAvailable]);

  if (!isAvailable) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center p-6 text-center bg-slate-900 text-slate-200">
        <div className="w-16 h-16 mb-4 flex items-center justify-center rounded-2xl bg-sky-500/10 text-sky-400 border border-sky-500/20 text-3xl">
          ✈️
        </div>
        <h1 className="text-xl font-bold mb-2 text-white">请在 Telegram 客户端中打开</h1>
        <p className="max-w-sm text-sm text-slate-400 leading-relaxed mb-6">
          Repo-Sentinel Mini App 专为 Telegram 移动端与桌面端设计。请通过 Telegram 机器人的推送通知或快捷菜单打开本应用。
        </p>
        <div className="rounded-lg bg-slate-800/80 border border-slate-700/60 p-4 text-xs text-slate-400 max-w-sm text-left">
          <p className="font-semibold text-slate-300 mb-1">💡 提示：</p>
          <ul className="list-disc list-inside space-y-1">
            <li>收到 GitHub 告警推送时点击「查看详情与处置」</li>
            <li>或在 Telegram 对话中点击 Bot 菜单按钮「打开看板」</li>
          </ul>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-[var(--tg-theme-bg-color,#0f172a)] text-[var(--tg-theme-text-color,#e2e8f0)] flex flex-col antialiased">
      <main className="flex-1 w-full max-w-md mx-auto p-4 pb-12">
        <Outlet />
      </main>
    </div>
  );
}

export default TmaLayout;
