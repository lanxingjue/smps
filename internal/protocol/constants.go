// internal/protocol/constants.go  # 协议常量定义
package protocol

// Command IDs
const (
	BIND_RECEIVER      = 0x00000001
	BIND_RECEIVER_RESP = 0x80000001
	DELIVER_SM         = 0x00000005
	DELIVER_SM_RESP    = 0x80000005
	ENQUIRE_LINK       = 0x00000015
	ENQUIRE_LINK_RESP  = 0x80000015
)

// Command status codes
const (
	SMPP_ESME_ROK = 0x00000000 // No Error
	SM_OK         = 0x00000000 // 允许发送
	SM_REJECT     = 0x00000001 // 拒绝发送
)

// 会话状态
const (
	Session_Start      = 0 // 开始创建
	Session_Connected  = 1 // 已连接
	Session_Disconnect = 2 // 断开
	Session_Closed     = 3 // 关闭
)
