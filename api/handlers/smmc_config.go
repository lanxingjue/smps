// api/handlers/smmc_config.go
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"smps/internal/auth"
)

// SMMCConfigHandler SMMC配置处理器
type SMMCConfigHandler struct {
	db            *sql.DB
	authenticator *auth.Authenticator
}

// NewSMMCConfigHandler 创建SMMC配置处理器
func NewSMMCConfigHandler(db *sql.DB, authenticator *auth.Authenticator) *SMMCConfigHandler {
	return &SMMCConfigHandler{
		db:            db,
		authenticator: authenticator,
	}
}

// SMMCConfig SMMC配置
type SMMCConfig struct {
	ID            int    `json:"id"`
	InterfaceName string `json:"interface_name" binding:"required"`
	SystemID      string `json:"system_id" binding:"required"`
	Password      string `json:"password" binding:"required"`
	IPAddress     string `json:"ip_address" binding:"required"`
	Port          int    `json:"port" binding:"required"`
	InterfaceType string `json:"interface_type" binding:"required"`
	Protocol      string `json:"protocol" binding:"required"`
	EncodingType  string `json:"encoding_type"`
	FlowControl   int    `json:"flow_control"`
	IsActive      bool   `json:"is_active"`
}

// ListConfigs 列出所有配置
func (h *SMMCConfigHandler) ListConfigs(c *gin.Context) {
	// 查询数据库
	rows, err := h.db.Query(`
		SELECT id, interface_name, system_id, password, ip_address, port, 
               interface_type, protocol, encoding_type, flow_control, is_active
		FROM external_smmc
		ORDER BY id
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var configs []SMMCConfig
	for rows.Next() {
		var config SMMCConfig
		if err := rows.Scan(
			&config.ID, &config.InterfaceName, &config.SystemID, &config.Password,
			&config.IPAddress, &config.Port, &config.InterfaceType, &config.Protocol,
			&config.EncodingType, &config.FlowControl, &config.IsActive,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		configs = append(configs, config)
	}

	c.JSON(http.StatusOK, configs)
}

// GetConfig 获取配置详情
func (h *SMMCConfigHandler) GetConfig(c *gin.Context) {
	idStr := c.Param("id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置ID"})
		return
	}

	// 查询数据库
	var config SMMCConfig
	err = h.db.QueryRow(`
		SELECT id, interface_name, system_id, password, ip_address, port, 
               interface_type, protocol, encoding_type, flow_control, is_active
		FROM external_smmc
		WHERE id = ?
	`, id).Scan(
		&config.ID, &config.InterfaceName, &config.SystemID, &config.Password,
		&config.IPAddress, &config.Port, &config.InterfaceType, &config.Protocol,
		&config.EncodingType, &config.FlowControl, &config.IsActive,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, config)
}

// CreateConfig 创建配置
func (h *SMMCConfigHandler) CreateConfig(c *gin.Context) {
	var config SMMCConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 插入数据库
	result, err := h.db.Exec(`
		INSERT INTO external_smmc (
			interface_name, system_id, password, ip_address, port, 
			interface_type, protocol, encoding_type, flow_control, is_active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		config.InterfaceName, config.SystemID, config.Password, config.IPAddress, config.Port,
		config.InterfaceType, config.Protocol, config.EncodingType, config.FlowControl, config.IsActive,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	id, _ := result.LastInsertId()
	config.ID = int(id)

	// 动态更新认证器
	acc := &auth.Account{
		SystemID:      config.SystemID,
		Password:      config.Password,
		InterfaceType: config.InterfaceType,
		Protocol:      config.Protocol,
		EncodingType:  config.EncodingType,
		FlowControl:   config.FlowControl,
	}
	h.authenticator.RegisterAccount(acc)

	c.JSON(http.StatusCreated, config)
}

// UpdateConfig 更新配置
func (h *SMMCConfigHandler) UpdateConfig(c *gin.Context) {
	idStr := c.Param("id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置ID"})
		return
	}

	var config SMMCConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新数据库
	_, err = h.db.Exec(`
		UPDATE external_smmc
		SET 
			interface_name = ?,
			system_id = ?,
			password = ?,
			ip_address = ?,
			port = ?,
			interface_type = ?,
			protocol = ?,
			encoding_type = ?,
			flow_control = ?,
			is_active = ?
		WHERE id = ?
	`,
		config.InterfaceName, config.SystemID, config.Password, config.IPAddress, config.Port,
		config.InterfaceType, config.Protocol, config.EncodingType, config.FlowControl, config.IsActive,
		id,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 动态更新认证器
	acc := &auth.Account{
		SystemID:      config.SystemID,
		Password:      config.Password,
		InterfaceType: config.InterfaceType,
		Protocol:      config.Protocol,
		EncodingType:  config.EncodingType,
		FlowControl:   config.FlowControl,
	}
	h.authenticator.RegisterAccount(acc)

	c.JSON(http.StatusOK, config)
}

// DeleteConfig 删除配置
func (h *SMMCConfigHandler) DeleteConfig(c *gin.Context) {
	idStr := c.Param("id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置ID"})
		return
	}

	// 查找配置信息
	var systemID string
	err = h.db.QueryRow("SELECT system_id FROM external_smmc WHERE id = ?", id).Scan(&systemID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 删除数据库记录
	_, err = h.db.Exec("DELETE FROM external_smmc WHERE id = ?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// TODO: 从认证器中移除账户

	c.JSON(http.StatusOK, gin.H{"message": "配置已删除"})
}
