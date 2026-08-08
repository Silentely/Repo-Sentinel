export interface ApiErrorInit {
  status: number;
  errorCode: string;
  message: string;
  details?: Record<string, unknown>;
}

// ApiError 只把后端安全说明放入 Error 文本，details 不参与字符串格式化。
export class ApiError extends Error {
  readonly status: number;
  readonly errorCode: string;
  readonly details?: Record<string, unknown>;

  constructor(init: ApiErrorInit) {
    super(init.message || "请求失败，请稍后重试。");
    this.name = "ApiError";
    this.status = init.status;
    this.errorCode = init.errorCode;
    this.details = init.details;
  }
}

export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error;
  }
  const name = errorNameOf(error);
  if (name === "TimeoutError") {
    return new ApiError({
      status: 0,
      errorCode: "timeout",
      message: "请求超时，服务可能正在忙，请稍后重试。",
    });
  }
  if (name === "AbortError") {
    return new ApiError({
      status: 0,
      errorCode: "request_aborted",
      message: "请求已取消。",
    });
  }
  return new ApiError({
    status: 0,
    errorCode: "network_error",
    message: "无法连接 RepoSentinel，请检查服务状态后重试。",
  });
}

function errorNameOf(error: unknown): string | undefined {
  if (typeof error !== "object" || error === null || !("name" in error)) {
    return undefined;
  }
  const name = (error as { name?: unknown }).name;
  return typeof name === "string" ? name : undefined;
}
