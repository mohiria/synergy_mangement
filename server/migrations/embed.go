// Package migrations 把 goose 迁移脚本嵌进二进制，供 cmd/migrate 在容器里离线执行。
//
// go:embed 不能跨父目录，所以嵌入声明只能放在迁移目录内。goose 与 sqlc 都只读 *.sql，
// 本文件对 `go tool goose -dir migrations` 与 `go tool sqlc generate` 均无影响。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
