// usermod 修改已有账号的系统管理员标记（#200，ADR 0003）。
// 这是管理员全部被撤销或停用后的应急恢复通道：只有能登服务器的人才能跑，本身就是一道授权。
// 用法：go run ./cmd/usermod -username alice -admin=true|false
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"synergy/server/internal/store"
)

func main() {
	username := flag.String("username", "", "登录用户名（必填）")
	admin := flag.Bool("admin", false, "设为 true 标为系统管理员，false 撤销（必须显式给出）")
	flag.Parse()

	if *username == "" || !flagGiven("admin") {
		flag.Usage()
		os.Exit(2)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	q := store.New(pool)
	u, err := q.GetUserByUsername(ctx, *username)
	if err != nil {
		log.Fatalf("user %q: %v", *username, err)
	}
	u, err = q.SetUserSystemAdmin(ctx, store.SetUserSystemAdminParams{ID: u.ID, IsSystemAdmin: *admin})
	if err != nil {
		log.Fatalf("update user: %v", err)
	}
	fmt.Printf("user #%d %s (%s) system_admin=%v\n", u.ID, u.Username, u.DisplayName, u.IsSystemAdmin)
}

// flagGiven 判断某个 flag 是否在命令行里显式出现，避免把「没传」当成「撤销」。
func flagGiven(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
