import { existsSync, readFileSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

// globalTeardown：删除本次运行由 webServer 命令创建的临时库目录。
// webServer 的 `mktemp -d .test-run-data/<name>.XXXXXX` 每次运行都新建库
// （保证首次设置等一次性流程可重复执行），若不自清理会在仓库根目录持续堆积。
const runDataDir = path.resolve(fileURLToPath(new URL("..", import.meta.url)), "..", ".test-run-data");
// webServer 的 cwd 是仓库根目录，清单里记录的是相对仓库根的路径。
const repoRoot = path.dirname(runDataDir);
const manifest = path.join(runDataDir, ".active-runs");

export default function globalTeardown(): void {
  if (!existsSync(manifest)) {
    return;
  }
  const recorded = readFileSync(manifest, "utf8")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");

  for (const dir of recorded) {
    const full = path.resolve(repoRoot, dir);
    // 只删除清单中记录、且确实位于 .test-run-data 下一层的运行目录，
    // 不触碰该目录下其他既有内容（历史产物、报告、登录态缓存等）。
    if (path.dirname(full) === runDataDir) {
      rmSync(full, { recursive: true, force: true });
    }
  }
  rmSync(manifest, { force: true });
}
