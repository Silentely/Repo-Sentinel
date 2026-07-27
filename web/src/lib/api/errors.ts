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
  return new ApiError({
    status: 0,
    errorCode: "network_error",
    message: "无法连接 RepoSentinel，请检查服务状态后重试。",
  });
}
