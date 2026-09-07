package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"synergy/server/internal/api"
	"synergy/server/internal/filestore"
	"synergy/server/internal/mail"
	"synergy/server/internal/secrets"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	// 文件服务：MinIO 预签名 URL（ADR 0001）。浏览器可达地址与内网地址不同时用 MINIO_PUBLIC_ENDPOINT。
	files, err := filestore.NewMinio(
		envOr("MINIO_ENDPOINT", "localhost:9000"),
		os.Getenv("MINIO_PUBLIC_ENDPOINT"),
		envOr("MINIO_ACCESS_KEY", "synergy"),
		envOr("MINIO_SECRET_KEY", "synergy-dev-secret"),
		envOr("MINIO_BUCKET", "synergy"),
		false,
	)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}
	if err := files.EnsureBucket(context.Background()); err != nil {
		log.Printf("warning: minio 不可达，文件上传下载暂不可用: %v", err)
	}

	// #212：应用密钥缺失时拒绝启动——SMTP 密码只能以密文落库（ADR 0003）。
	secretKey, err := secrets.KeyFromEnv()
	if err != nil {
		log.Fatalf("%v", err)
	}

	srv := api.NewServer(pool, files)
	srv.ConfigureMail(secretKey, mail.SMTPSender{})
	// 邮件 outbox：进程内单协程按间隔取出发送（模块 PRD §10.2）。
	stopMail := srv.StartMailOutboxWorker(context.Background(), api.MailOutboxInterval)
	defer stopMail()
	// 时间型卡点的动态留痕（ADR 0001）：进程内单 ticker 每小时扫描活跃项目补记，无外部定时设施。
	stopSweep := srv.StartBlockerActivityTicker(context.Background(), api.BlockerSweepInterval)
	defer stopSweep()
	handler := api.NewHandlerFromServer(srv, "/api/v1")

	// 显式设四个超时（E3）：默认的 ListenAndServe 一个都不设，慢连接就能一直占住
	// 连接与数据库连接。写超时要盖住报告导出这类同步长任务，故单独放宽。
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("listening on %s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
