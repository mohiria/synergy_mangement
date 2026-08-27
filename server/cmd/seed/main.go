// seed 重置演示数据：清空全部业务数据（含用户）后按 sql/ 目录里的脚本重建一整套。
//
// 用法：
//
//	go run ./cmd/seed                 # 重建数据并补齐 MinIO 里的占位文件
//	go run ./cmd/seed -skip-files     # 只重建数据库
//
// 数据库连接取 DATABASE_URL，MinIO 取 MINIO_* 环境变量，默认值与 cmd/server 一致。
// 演示账号的统一口令取 SEED_PASSWORD（必填，仓库里不存明文）：
//
//	$env:SEED_PASSWORD = '你的口令'; go run ./cmd/seed
//
// 全部 SQL 在一个事务里跑，任一步失败整体回滚，不会留下半套数据。
package main

import (
	"bytes"
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/filestore"
)

//go:embed sql/*.sql
var scripts embed.FS

// seedPasswordEnv 提供全部演示账号的初始口令；SQL 里用 current_setting 读，哈希由 pgcrypto 现算。
const seedPasswordEnv = "SEED_PASSWORD"

// seedPasswordSetting 是口令在事务内的配置项名，与 sql/01_org.sql 里的 current_setting 对应。
const seedPasswordSetting = "synergy.seed_password"

func main() {
	skipFiles := flag.Bool("skip-files", false, "跳过 MinIO 占位文件上传")
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx, *skipFiles); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, skipFiles bool) error {
	password := os.Getenv(seedPasswordEnv)
	if password == "" {
		return fmt.Errorf("请先设置 %s 环境变量，作为全部演示账号的初始口令", seedPasswordEnv)
	}

	conn, err := connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	names, err := scriptNames()
	if err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 口令以事务级配置项传进去，SQL 脚本里不出现明文。
	if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", seedPasswordSetting, password); err != nil {
		return fmt.Errorf("set %s: %w", seedPasswordSetting, err)
	}
	for _, name := range names {
		body, err := scripts.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
		log.Printf("已执行 %s", name)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if !skipFiles {
		if err := seedObjects(ctx, conn); err != nil {
			// 文件是演示用的占位内容，MinIO 不可达不该让整次重置失败。
			log.Printf("警告：占位文件未上传（%v），页面上的下载会失败，数据本身没问题", err)
		}
	}
	return summarize(ctx, conn, password)
}

// connect 用简单查询协议连接：种子脚本是多语句文本，扩展协议一次只能发一条。
func connect(ctx context.Context) (*pgx.Conn, error) {
	dsn := envOr("DATABASE_URL", "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable")
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return conn, nil
}

func scriptNames() ([]string, error) {
	entries, err := scripts.ReadDir("sql")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, "sql/"+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// seedObjects 为每个对象键写一份占位内容，让下载、成果包打包这些路径在演示时也能走通。
func seedObjects(ctx context.Context, conn *pgx.Conn) error {
	files, err := filestore.NewMinio(
		envOr("MINIO_ENDPOINT", "localhost:9000"),
		os.Getenv("MINIO_PUBLIC_ENDPOINT"),
		envOr("MINIO_ACCESS_KEY", "synergy"),
		envOr("MINIO_SECRET_KEY", "synergy-dev-secret"),
		envOr("MINIO_BUCKET", "synergy"),
		false,
	)
	if err != nil {
		return fmt.Errorf("minio client: %w", err)
	}
	if err := files.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("minio 不可达: %w", err)
	}

	rows, err := conn.Query(ctx, `
        SELECT object_key, file_name FROM deliverable_files WHERE object_key <> ''
        UNION ALL
        SELECT object_key, file_name FROM input_requests WHERE object_key <> ''`)
	if err != nil {
		return fmt.Errorf("列出对象键: %w", err)
	}
	type object struct{ key, name string }
	var objects []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.key, &o.name); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, o := range objects {
		body := []byte(fmt.Sprintf("演示数据占位文件：%s\n内容为空，仅用于验证上传、下载与成果包打包链路。\n", o.name))
		if err := files.Put(ctx, o.key, bytes.NewReader(body), int64(len(body))); err != nil {
			return fmt.Errorf("上传 %s: %w", o.key, err)
		}
	}
	log.Printf("已写入 %d 个占位文件", len(objects))
	return nil
}

func summarize(ctx context.Context, conn *pgx.Conn, password string) error {
	tables := []string{
		"users", "projects", "project_members", "objectives", "key_results", "tasks",
		"pool_reviews", "completion_reviews", "field_change_requests", "task_invites",
		"deliverables", "deliverable_files", "deliverable_edges", "input_requests",
		"task_receivers", "task_receipts", "discussions", "notifications",
		"task_activities", "artifact_packages", "remind_logs",
	}
	total := 0
	var b strings.Builder
	for _, t := range tables {
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return fmt.Errorf("统计 %s: %w", t, err)
		}
		total += n
		fmt.Fprintf(&b, "  %-22s %4d\n", t, n)
	}
	fmt.Printf("演示数据已重建，合计 %d 行：\n%s", total, b.String())
	fmt.Printf("全部账号口令：%s（来自 %s）\n", password, seedPasswordEnv)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
