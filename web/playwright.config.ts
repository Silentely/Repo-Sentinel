import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";

const repositoryRoot = fileURLToPath(new URL("..", import.meta.url));
const desktopPort = 34_117;
const mobilePort = 34_118;

function serverCommand(name: string, port: number): string {
  return [
    "sh -c '",
    "mkdir -p .test-run-data && ",
    `e2e_dir=$(mktemp -d .test-run-data/${name}.XXXXXX) && `,
    // 记录本次运行的临时库目录，由 globalTeardown 在运行结束后删除：
    // 每次运行都新建库（保证首次设置流程可跑），因此必须自清理，否则会持续堆积。
    "echo \"$e2e_dir\" >> .test-run-data/.active-runs && ",
    `REPOSENTINEL_HTTP_ADDR=127.0.0.1:${port} `,
    `REPOSENTINEL_PUBLIC_BASE_URL=http://127.0.0.1:${port} `,
    "REPOSENTINEL_DATABASE_DRIVER=sqlite ",
    "REPOSENTINEL_DATABASE_MAX_OPEN_CONNS=1 ",
    "REPOSENTINEL_DATABASE_MAX_IDLE_CONNS=1 ",
    "REPOSENTINEL_LOG_FORMAT=json ",
    "REPOSENTINEL_LOG_LEVEL=error ",
    "REPOSENTINEL_ENCRYPTION_KEY=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff ",
    'REPOSENTINEL_DATABASE_URL="file:$e2e_dir/reposentinel.db" ',
    "exec go run -tags production ./cmd/reposentinel serve'",
  ].join("");
}

export default defineConfig({
  testDir: "./e2e",
  outputDir: "../.test-run-data/playwright-artifacts",
  // 每次运行结束后删除本次运行创建的临时库目录（见 global-teardown.ts）。
  globalTeardown: "./e2e/global-teardown.ts",
  // 实例为单管理员模型且首次设置只能完成一次：各用例文件共用同一服务与数据库，
  // 必须串行执行（fullyParallel:false 仅保证文件内串行，跨文件仍会并发）。
  workers: 1,
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "../.test-run-data/playwright-report" }]],
  use: {
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: [
    {
      command: serverCommand("desktop", desktopPort),
      cwd: repositoryRoot,
      url: `http://127.0.0.1:${desktopPort}/health/live`,
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: serverCommand("mobile", mobilePort),
      cwd: repositoryRoot,
      url: `http://127.0.0.1:${mobilePort}/health/live`,
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
  projects: [
    {
      name: "chromium-desktop",
      use: {
        ...devices["Desktop Chrome"],
        baseURL: `http://127.0.0.1:${desktopPort}`,
      },
    },
    {
      name: "chromium-mobile",
      use: {
        ...devices["Pixel 7"],
        baseURL: `http://127.0.0.1:${mobilePort}`,
      },
    },
  ],
});
