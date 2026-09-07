// useradd 创建本地账号（契约无注册端点；成员与角色管理属 ticket #2）。
// 用法：go run ./cmd/useradd -username alice -display 张三 -email alice@example.com -password s3cret [-admin]
// -admin 标为系统管理员（#200）：首个管理员只能由此产生；已有用户改标记用 ./cmd/usermod。
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
	email := flag.String("email", "", "邮箱（必填，全局唯一，大小写不敏感）")
	admin := flag.Bool("admin", false, "标为系统管理员：隐式视同所有项目的管理员，可进系统设置")
	flag.Parse()

	if *username == "" || *display == "" || *password == "" || *email == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := domain.ValidateEmail(*email); err != nil {
		log.Fatalf("email: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	if err := domain.ValidatePasswordLength(*password); err != nil {
		log.Fatalf("password: %v", err)
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
		Username:      *username,
		DisplayName:   *display,
		PasswordHash:  hash,
		IsSystemAdmin: *admin,
		Email:         domain.NormalizeEmail(*email),
	})
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("created user #%d %s (%s) system_admin=%v\n", u.ID, u.Username, u.DisplayName, u.IsSystemAdmin)
}
