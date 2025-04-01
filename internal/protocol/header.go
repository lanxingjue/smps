// internal/protocol/header.go  # SMPP消息头部
package protocol

import (
	"encoding/binary"
	"io"
)

// SMPPHeader 定义SMPP消息头
type SMPPHeader struct {
	CommandLength  uint32 // 消息总长度
	CommandID      uint32 // 命令ID
	CommandStatus  uint32 // 状态码
	SequenceNumber uint32 // 序列号
}

// Write 将头部信息写入写入器
func (h *SMPPHeader) Write(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, h)
}

// Read 从读取器中读取头部信息
func (h *SMPPHeader) Read(r io.Reader) error {
	return binary.Read(r, binary.BigEndian, h)
}
