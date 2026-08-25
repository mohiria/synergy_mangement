package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"synergy/server/internal/api"
	"synergy/server/internal/filestore"
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

	handler := api.NewHandler(pool, "/api/v1", files)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
