// internal/tracer/storage.go
package tracer

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ProtocolStorage 协议存储接口
type ProtocolStorage interface {
	// Store 存储日志
	Store(log *ProtocolLog) error

	// Query 查询日志
	Query(options QueryOptions) ([]*ProtocolLog, error)

	// Clear 清除日志
	Clear() error
}

// QueryOptions 查询选项
type QueryOptions struct {
	StartTime  time.Time
	EndTime    time.Time
	SourceAddr string
	DestAddr   string
	SystemID   string
	MessageID  uint32
	Limit      int
	Offset     int
}

// MemoryProtocolStorage 内存存储实现
type MemoryProtocolStorage struct {
	logs []*ProtocolLog
	mu   sync.RWMutex
}

// Store 存储日志
func (s *MemoryProtocolStorage) Store(log *ProtocolLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = append(s.logs, log)

	// 如果日志数量超过1000条，删除最旧的
	if len(s.logs) > 1000 {
		s.logs = s.logs[1:]
	}

	return nil
}

// Query 查询日志
func (s *MemoryProtocolStorage) Query(options QueryOptions) ([]*ProtocolLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ProtocolLog

	for _, log := range s.logs {
		// 时间范围过滤
		if !options.StartTime.IsZero() && log.Timestamp.Before(options.StartTime) {
			continue
		}

		if !options.EndTime.IsZero() && log.Timestamp.After(options.EndTime) {
			continue
		}

		// 源地址过滤
		if options.SourceAddr != "" && log.SourceAddr != options.SourceAddr {
			continue
		}

		// 目标地址过滤
		if options.DestAddr != "" && log.DestAddr != options.DestAddr {
			continue
		}

		// 系统ID过滤
		if options.SystemID != "" && log.SystemID != options.SystemID {
			continue
		}

		// 消息ID过滤
		if options.MessageID != 0 && log.MessageID != options.MessageID {
			continue
		}

		result = append(result, log)
	}

	// 应用分页
	if options.Limit > 0 {
		end := options.Offset + options.Limit
		if end > len(result) {
			end = len(result)
		}

		if options.Offset < end {
			result = result[options.Offset:end]
		} else {
			result = []*ProtocolLog{}
		}
	}

	return result, nil
}

// Clear 清除日志
func (s *MemoryProtocolStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = make([]*ProtocolLog, 0, 1000)
	return nil
}

// DatabaseProtocolStorage 数据库存储实现
type DatabaseProtocolStorage struct {
	db *sql.DB
}

// NewDatabaseProtocolStorage 创建数据库存储
func NewDatabaseProtocolStorage(db *sql.DB) *DatabaseProtocolStorage {
	return &DatabaseProtocolStorage{
		db: db,
	}
}

// Store 存储日志
func (s *DatabaseProtocolStorage) Store(log *ProtocolLog) error {
	query := `
		INSERT INTO protocol_logs 
		(timestamp, direction, message_id, command_name, source_addr, dest_addr, content, raw_data, system_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(
		query,
		log.Timestamp,
		log.Direction,
		log.MessageID,
		log.CommandName,
		log.SourceAddr,
		log.DestAddr,
		log.Content,
		log.RawData,
		log.SystemID,
		log.Status,
	)

	return err
}

// Query 查询日志
func (s *DatabaseProtocolStorage) Query(options QueryOptions) ([]*ProtocolLog, error) {
	query := `
		SELECT timestamp, direction, message_id, command_name, source_addr, dest_addr, content, raw_data, system_id, status
		FROM protocol_logs
		WHERE 1=1
	`

	var params []interface{}

	if !options.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		params = append(params, options.StartTime)
	}

	if !options.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		params = append(params, options.EndTime)
	}

	if options.SourceAddr != "" {
		query += " AND source_addr = ?"
		params = append(params, options.SourceAddr)
	}

	if options.DestAddr != "" {
		query += " AND dest_addr = ?"
		params = append(params, options.DestAddr)
	}

	if options.SystemID != "" {
		query += " AND system_id = ?"
		params = append(params, options.SystemID)
	}

	if options.MessageID != 0 {
		query += " AND message_id = ?"
		params = append(params, options.MessageID)
	}

	query += " ORDER BY timestamp DESC"

	if options.Limit > 0 {
		query += " LIMIT ?, ?"
		params = append(params, options.Offset, options.Limit)
	}

	rows, err := s.db.Query(query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*ProtocolLog

	for rows.Next() {
		log := &ProtocolLog{}
		var rawData []byte

		err := rows.Scan(
			&log.Timestamp,
			&log.Direction,
			&log.MessageID,
			&log.CommandName,
			&log.SourceAddr,
			&log.DestAddr,
			&log.Content,
			&rawData,
			&log.SystemID,
			&log.Status,
		)
		if err != nil {
			return nil, err
		}

		log.RawData = rawData
		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return logs, nil
}

// Clear 清除日志
func (s *DatabaseProtocolStorage) Clear() error {
	_, err := s.db.Exec("DELETE FROM protocol_logs")
	return err
}

// LoadTracedNumbersFromDB 从数据库加载跟踪号码
func (t *ProtocolTracer) LoadTracedNumbersFromDB() error {
	if t.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 查询跟踪号码
	rows, err := t.db.Query("SELECT number FROM traced_numbers")
	if err != nil {
		return fmt.Errorf("查询跟踪号码失败: %v", err)
	}
	defer rows.Close()

	// 清空现有跟踪号码
	t.config.mu.Lock()
	t.config.TracedNumbers = make(map[string]bool)

	// 加载跟踪号码
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			t.config.mu.Unlock()
			return fmt.Errorf("扫描跟踪号码失败: %v", err)
		}

		t.config.TracedNumbers[number] = true
	}
	t.config.mu.Unlock()

	return nil
}

// SaveTracedNumber 保存跟踪号码到数据库
func (t *ProtocolTracer) SaveTracedNumber(number string) error {
	if t.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 检查号码是否已存在
	var count int
	err := t.db.QueryRow("SELECT COUNT(*) FROM traced_numbers WHERE number = ?", number).Scan(&count)
	if err != nil {
		return fmt.Errorf("检查跟踪号码失败: %v", err)
	}

	if count == 0 {
		// 插入新号码
		_, err = t.db.Exec("INSERT INTO traced_numbers (number) VALUES (?)", number)
		if err != nil {
			return fmt.Errorf("保存跟踪号码失败: %v", err)
		}
	}

	// 添加到内存
	t.AddTracedNumber(number)

	return nil
}

// DeleteTracedNumber 从数据库删除跟踪号码
func (t *ProtocolTracer) DeleteTracedNumber(number string) error {
	if t.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 从数据库删除
	_, err := t.db.Exec("DELETE FROM traced_numbers WHERE number = ?", number)
	if err != nil {
		return fmt.Errorf("删除跟踪号码失败: %v", err)
	}

	// 从内存移除
	t.RemoveTracedNumber(number)

	return nil
}

// LoadConfigFromDB 从数据库加载跟踪配置
func (t *ProtocolTracer) LoadConfigFromDB() error {
	if t.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 查询配置
	rows, err := t.db.Query("SELECT config_key, config_value FROM system_config WHERE config_key IN ('trace_enabled', 'parse_content')")
	if err != nil {
		return fmt.Errorf("查询跟踪配置失败: %v", err)
	}
	defer rows.Close()

	// 加载配置
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("扫描配置数据失败: %v", err)
		}

		switch key {
		case "trace_enabled":
			t.SetEnabled(value == "true")
		case "parse_content":
			t.SetParseContent(value == "true")
		}
	}

	return nil
}

// SaveConfigToDB 保存跟踪配置到数据库
func (t *ProtocolTracer) SaveConfigToDB() error {
	if t.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 获取当前配置
	enabled := t.IsEnabled()
	parseContent := t.IsParseContentEnabled()

	// 更新trace_enabled配置
	_, err := t.db.Exec(
		"UPDATE system_config SET config_value = ? WHERE config_key = 'trace_enabled'",
		fmt.Sprintf("%t", enabled),
	)
	if err != nil {
		return fmt.Errorf("更新trace_enabled配置失败: %v", err)
	}

	// 更新parse_content配置
	_, err = t.db.Exec(
		"UPDATE system_config SET config_value = ? WHERE config_key = 'parse_content'",
		fmt.Sprintf("%t", parseContent),
	)
	if err != nil {
		return fmt.Errorf("更新parse_content配置失败: %v", err)
	}

	return nil
}
