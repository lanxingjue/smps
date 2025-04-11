// internal/database/migration.go
package database

import (
	"database/sql"
	"fmt"

	"smps/pkg/logger"
)

// Migration 数据库迁移
type Migration struct {
	ID      int
	Name    string
	SQL     string
	Applied bool
}

// Migrate 执行数据库迁移
func Migrate(db *sql.DB) error {
	// 首先创建迁移记录表
	err := createMigrationsTable(db)
	if err != nil {
		return err
	}

	// 定义迁移列表
	migrations := []Migration{
		{
			ID:   1,
			Name: "create_initial_tables",
			SQL:  "", // 这个迁移通过CreateTables函数执行
		},
		{
			ID:   2,
			Name: "add_message_status_column",
			SQL:  "ALTER TABLE protocol_logs ADD COLUMN is_processed BOOLEAN DEFAULT 0 AFTER status;",
		},
		// 更多迁移可以在这里添加
	}

	// 执行迁移
	for _, migration := range migrations {
		applied, err := isMigrationApplied(db, migration.ID)
		if err != nil {
			return err
		}

		if !applied {
			logger.Info(fmt.Sprintf("执行迁移: %s", migration.Name))

			if migration.ID == 1 {
				// 特殊处理第一个迁移，调用CreateTables
				if err := CreateTables(db); err != nil {
					return err
				}
			} else if migration.SQL != "" {
				// 执行SQL
				if _, err := db.Exec(migration.SQL); err != nil {
					return fmt.Errorf("执行迁移%d失败: %v", migration.ID, err)
				}
			}

			// 记录迁移已应用
			if err := recordMigration(db, migration.ID, migration.Name); err != nil {
				return err
			}

			logger.Info(fmt.Sprintf("迁移完成: %s", migration.Name))
		}
	}

	return nil
}

// createMigrationsTable 创建迁移记录表
func createMigrationsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS migrations (
			id INT NOT NULL,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("创建migrations表失败: %v", err)
	}

	return nil
}

// isMigrationApplied 检查迁移是否已应用
func isMigrationApplied(db *sql.DB, id int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM migrations WHERE id = ?", id).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查迁移状态失败: %v", err)
	}

	return count > 0, nil
}

// recordMigration 记录迁移已应用
func recordMigration(db *sql.DB, id int, name string) error {
	_, err := db.Exec("INSERT INTO migrations (id, name) VALUES (?, ?)", id, name)
	if err != nil {
		return fmt.Errorf("记录迁移状态失败: %v", err)
	}

	return nil
}
