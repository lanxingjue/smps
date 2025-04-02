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

// 补充绑定状态码
const (
	E_SUCCESS           = 0x00000000 // 连接成功
	E_OTHERERR          = 0x00000001 // 未指定类型错误
	E_PASSWORD          = 0x00000002 // 密码错误
	E_SYSTEMID          = 0x00000003 // 接口的标识符错误
	E_SYSTEMTYPE        = 0x00000004 // 接口的类型错误
	E_INTERFACE_VERSION = 0x00000005 // 接口的版本号错误
)

// 补充解绑相关常量
const (
	UNBIND      = 0x00000006
	UNBIND_RESP = 0x80000006
	UNBIND_OK   = 0x00000000 // 断开连接成功
)
