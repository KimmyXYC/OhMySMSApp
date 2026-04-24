// Package db 负责打开 SQLite 连接并执行嵌入式迁移。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite 驱动
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open 打开（必要时创建）SQLite 库，启用 WAL/外键，并执行未应用的迁移。
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1) // 简化：串行写。读走内存缓存已足够
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return err
		}
		// 外键在事务内无法切换；某些迁移需要重建表（INSERT SELECT → DROP → RENAME），
		// 若开着 FK 会被子表 CASCADE/SET NULL 反噬。因此每次 migration 前临时关闭 FK，
		// 事务结束后恢复。对不涉及重建的迁移（如 0001/0002 纯 CREATE/ALTER ADD COLUMN）无副作用。
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return fmt.Errorf("disable fk before %s: %w", name, err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			_, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
			return err
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()
			_, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version) VALUES(?)`, version,
		); err != nil {
			tx.Rollback()
			_, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
			return err
		}
		if err := tx.Commit(); err != nil {
			_, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
			return err
		}
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
			return fmt.Errorf("re-enable fk after %s: %w", name, err)
		}
	}
	return nil
}
