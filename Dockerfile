# RepoSentinel 多阶段生产镜像
# syntax=docker/dockerfile:1.7

FROM node:24-bookworm AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@10.34.5 --activate \
  && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# 仅复制构建所需路径，配合 .dockerignore 缩小上下文并提升缓存命中率
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY web/*.go ./web/
COPY --from=frontend /src/web/dist ./web/dist
# 默认版本与根目录 VERSION 保持一致（CI 经 build-arg 覆盖）；本地 docker build 不应产出错误版本号。
ARG VERSION=0.3.8
ARG GIT_SHA=unknown
ARG GIT_BRANCH=unknown
ARG BUILD_TIME=unknown
ARG BUILD_CHANNEL=release
RUN CGO_ENABLED=0 GOOS=linux go build -tags production \
  -ldflags "-s -w \
    -X github.com/Silentely/Repo-Sentinel/internal/buildinfo.version=${VERSION} \
    -X github.com/Silentely/Repo-Sentinel/internal/buildinfo.gitSHA=${GIT_SHA} \
    -X github.com/Silentely/Repo-Sentinel/internal/buildinfo.gitBranch=${GIT_BRANCH} \
    -X github.com/Silentely/Repo-Sentinel/internal/buildinfo.buildTime=${BUILD_TIME} \
    -X github.com/Silentely/Repo-Sentinel/internal/buildinfo.buildChannel=${BUILD_CHANNEL}" \
  -o /out/reposentinel ./cmd/reposentinel

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=backend /out/reposentinel /reposentinel
USER nonroot:nonroot
EXPOSE 8080
ENV REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
    REPOSENTINEL_DATABASE_DRIVER=sqlite \
    REPOSENTINEL_DATABASE_URL=file:/data/reposentinel.db
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/reposentinel", "healthcheck"]
ENTRYPOINT ["/reposentinel"]
CMD ["serve"]
