# 数据库操作服务（db-ops）· Go
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/db-ops .

FROM alpine:3.20
RUN adduser -D -u 10001 app
WORKDIR /app
# 迁移目录由部署侧挂载（K8s configMap / compose volume），镜像里只建空目录占位
RUN mkdir -p /app/migrations && chown app /app/migrations
COPY --from=build /out/db-ops /app/db-ops
USER app
EXPOSE 8091
ENV SERVER_PORT=8091
ENV DBOPS_MIGRATIONS_DIR=/app/migrations
ENTRYPOINT ["/app/db-ops"]
