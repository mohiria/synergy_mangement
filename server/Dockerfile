FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server
# 迁移入口与 app 打进同一镜像，compose 的 migrate 服务覆盖 entrypoint 复用它。
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate
# useradd 用来在空库上创建第一个账号：docker compose run --rm --entrypoint /useradd app ...
RUN CGO_ENABLED=0 go build -o /out/useradd ./cmd/useradd

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
COPY --from=build /out/migrate /migrate
COPY --from=build /out/useradd /useradd
EXPOSE 8080
ENTRYPOINT ["/server"]
