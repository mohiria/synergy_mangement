FROM golang:1.26 AS build
WORKDIR /src
# 默认走国内代理（部署目标在国内，直连 proxy.golang.org 会超时）；direct 兜底。
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server
# 迁移入口与 app 打进同一镜像，compose 的 migrate 服务覆盖 entrypoint 复用它。
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate
# useradd 用来在空库上创建第一个账号：docker compose run --rm --entrypoint /useradd app ...
RUN CGO_ENABLED=0 go build -o /out/useradd ./cmd/useradd

# 运行层用 scratch：二进制是 CGO_ENABLED=0 的静态包，只需补根证书与时区。
# 不用 gcr.io/distroless（国内网络拉不到），效果等价。
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/server /server
COPY --from=build /out/migrate /migrate
COPY --from=build /out/useradd /useradd
EXPOSE 8080
ENTRYPOINT ["/server"]
