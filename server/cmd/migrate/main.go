// migrate 把数据库结构迁移到最新版本。部署时由 docker-compose 的 migrate 服务先跑一次，
// 成功退出后 app 才启动（depends_on: service_completed_successfully）。
//
// 用法：
//
//	migrate            # 迁移到最新（goose up）
//	migrate -status    # 只打印迁移状态，不改库
//
// 迁移脚本编译进二进制（见 server/migrations/embed.go），容器里不需要挂载 migrations 目录。
// 连接串取 DATABASE_URL，默认值与 cmd/server 一致。
package main

import (
	"database/sql"
	"flag"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"synergy/server/migrations"
)

func main() {
	status := flag.Bool("status", false, "只打印迁移状态，不执行迁移")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		log.Fatalf("Postgres 不可达: %v", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("goose dialect: %v", err)
	}

	if *status {
		if err := goose.Status(db, "."); err != nil {
			log.Fatalf("goose status: %v", err)
		}
		return
	}
	if err := goose.Up(db, "."); err != nil {
		log.Fatalf("goose up: %v", err)
	}
	log.Println("数据库已迁移到最新版本")
}
