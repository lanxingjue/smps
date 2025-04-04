// internal/protocol/message.go   消息结构
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// TLV 表示一个TLV参数
type TLV struct {
	Tag   uint16
	Len   uint16
	Value []byte
}

// Message 表示一个完整的SMPP消息
type Message struct {
	Header  SMPPHeader
	Payload []byte
	TLVs    []TLV
}

// NewMessage 创建新消息
func NewMessage(commandID, status, sequence uint32, payload []byte) *Message {
	return &Message{
		Header: SMPPHeader{
			CommandLength:  uint32(16 + len(payload)),
			CommandID:      commandID,
			CommandStatus:  status,
			SequenceNumber: sequence,
		},
		Payload: payload,
	}
}

// Bytes 将消息转换为字节数组
func (m *Message) Bytes() []byte {
	// 重新计算总长度
	totalLen := 16 + len(m.Payload)
	for _, tlv := range m.TLVs {
		totalLen += 4 + int(tlv.Len)
	}

	m.Header.CommandLength = uint32(totalLen)

	// 序列化消息
	buf := new(bytes.Buffer)
	m.Header.Write(buf)
	buf.Write(m.Payload)

	// 添加所有TLV
	for _, tlv := range m.TLVs {
		binary.Write(buf, binary.BigEndian, tlv.Tag)
		binary.Write(buf, binary.BigEndian, tlv.Len)
		buf.Write(tlv.Value)
	}

	return buf.Bytes()
}

// CreateBindResponse 创建绑定响应消息
func CreateBindResponse(sequence uint32, status uint32, systemID string) *Message {
	payload := append([]byte(systemID), 0) // 添加空终止符
	return NewMessage(BIND_RECEIVER_RESP, status, sequence, payload)
}

// CreateDeliverSMResponse 创建短信投递响应
func CreateDeliverSMResponse(sequence uint32, status uint32) *Message {
	return NewMessage(DELIVER_SM_RESP, status, sequence, nil)
}

// CreateEnquireLinkResponse 创建链路查询响应
func CreateEnquireLinkResponse(sequence uint32) *Message {
	return NewMessage(ENQUIRE_LINK_RESP, SMPP_ESME_ROK, sequence, nil)
}

// AddTLV 添加TLV参数到消息
func (m *Message) AddTLV(tag uint16, value []byte) {
	tlv := TLV{
		Tag:   tag,
		Len:   uint16(len(value)),
		Value: value,
	}

	m.TLVs = append(m.TLVs, tlv)
	m.Header.CommandLength += uint32(4 + len(value)) // 更新消息长度
}

// CreateEnquireLink 创建链路查询请求
func CreateEnquireLink(sequence uint32) *Message {
	return NewMessage(ENQUIRE_LINK, 0, sequence, nil)
}

// CreateUnbindResponse 创建解绑响应
func CreateUnbindResponse(sequence uint32, status uint32) *Message {
	return NewMessage(UNBIND_RESP, status, sequence, nil)
}

// ParseMessage 从字节数组解析消息
func ParseMessage(data []byte) (*Message, error) {
	if len(data) < 16 {
		return nil, errors.New("invalid message: too short")
	}

	// 解析消息头
	header := SMPPHeader{
		CommandLength:  binary.BigEndian.Uint32(data[0:4]),
		CommandID:      binary.BigEndian.Uint32(data[4:8]),
		CommandStatus:  binary.BigEndian.Uint32(data[8:12]),
		SequenceNumber: binary.BigEndian.Uint32(data[12:16]),
	}

	if uint32(len(data)) < header.CommandLength {
		return nil, errors.New("invalid message: incomplete data")
	}

	// 构建消息对象
	message := &Message{
		Header:  header,
		Payload: data[16:header.CommandLength],
	}

	return message, nil
}

// CloneMessage 克隆消息
func CloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}

	// 复制负载
	payload := make([]byte, len(msg.Payload))
	copy(payload, msg.Payload)

	// 创建新消息
	newMsg := &Message{
		Header: SMPPHeader{
			CommandLength:  msg.Header.CommandLength,
			CommandID:      msg.Header.CommandID,
			CommandStatus:  msg.Header.CommandStatus,
			SequenceNumber: msg.Header.SequenceNumber,
		},
		Payload: payload,
	}

	// 复制TLV
	if len(msg.TLVs) > 0 {
		newMsg.TLVs = make([]TLV, len(msg.TLVs))
		for i, tlv := range msg.TLVs {
			// 复制TLV值
			value := make([]byte, len(tlv.Value))
			copy(value, tlv.Value)

			newMsg.TLVs[i] = TLV{
				Tag:   tlv.Tag,
				Len:   tlv.Len,
				Value: value,
			}
		}
	}

	return newMsg
}

// GetCommandName 获取命令名称
func GetCommandName(commandID uint32) string {
	switch commandID {
	case BIND_RECEIVER:
		return "BIND_RECEIVER"
	case BIND_RECEIVER_RESP:
		return "BIND_RECEIVER_RESP"
	case DELIVER_SM:
		return "DELIVER_SM"
	case DELIVER_SM_RESP:
		return "DELIVER_SM_RESP"
	case ENQUIRE_LINK:
		return "ENQUIRE_LINK"
	case ENQUIRE_LINK_RESP:
		return "ENQUIRE_LINK_RESP"
	case UNBIND:
		return "UNBIND"
	case UNBIND_RESP:
		return "UNBIND_RESP"
	default:
		return fmt.Sprintf("UNKNOWN(0x%08X)", commandID)
	}
}

// ParseMessageContent 解析消息内容
func ParseMessageContent(msg *Message) (sourceAddr, destAddr, content string, err error) {
	// 仅处理短信投递消息
	if msg.Header.CommandID != DELIVER_SM || len(msg.Payload) < 2 {
		return "", "", "", errors.New("非短信投递消息或负载无效")
	}

	// 解析消息内容
	fields := bytes.Split(msg.Payload, []byte{0})
	if len(fields) < 4 {
		return "", "", "", errors.New("无效的消息格式")
	}

	// 第二个字段是源地址
	if len(fields) > 1 {
		sourceAddr = string(fields[1])
	}

	// 第三个字段是目标地址
	if len(fields) > 2 {
		destAddr = string(fields[2])
	}

	// 第四个字段是消息内容
	if len(fields) > 3 {
		content = string(fields[3])
	}

	return
}
