// useradd 创建本地账号（契约无注册端点；成员与角色管理属 ticket #2）。
// 用法：go run ./cmd/useradd -username alice -display 张三 -password s3cret
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

func main() {
	username := flag.String("username", "", "登录用户名（必填）")
	display := flag.String("display", "", "显示名（必填）")
	password := flag.String("password", "", "初始密码（必填）")
	flag.Parse()

	if *username == "" || *display == "" || *password == "" {
		flag.Usage()
		os.Exit(2)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	hash, err := domain.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	u, err := store.New(pool).CreateUser(ctx, store.CreateUserParams{
		Username:     *username,
		DisplayName:  *display,
		PasswordHash: hash,
	})
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("created user #%d %s (%s)\n", u.ID, u.Username, u.DisplayName)
}
