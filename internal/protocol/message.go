// internal/protocol/message.go   消息结构
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
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
