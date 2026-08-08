import { describe, expect, it } from "vitest";

import { toApiError } from "./errors";

describe("toApiError", () => {
  it("将请求超时归类为 timeout，并提示用户稍后重试", () => {
    const error = Object.assign(new Error("signal timed out"), { name: "TimeoutError" });

    expect(toApiError(error)).toMatchObject({
      status: 0,
      errorCode: "timeout",
      message: "请求超时，服务可能正在忙，请稍后重试。",
    });
  });

  it("将主动取消归类为 request_aborted，避免误报为网络故障", () => {
    const error = Object.assign(new Error("operation aborted"), { name: "AbortError" });

    expect(toApiError(error)).toMatchObject({
      status: 0,
      errorCode: "request_aborted",
      message: "请求已取消。",
    });
  });

  it("普通异常仍使用网络错误的通用提示", () => {
    expect(toApiError(new Error("connection refused"))).toMatchObject({
      status: 0,
      errorCode: "network_error",
      message: "无法连接 RepoSentinel，请检查服务状态后重试。",
    });
  });
});
