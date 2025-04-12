// cmd/smpp_client/main.go
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	address := flag.String("address", "localhost:5016", "服务器地址")
	systemID := flag.String("system-id", "smmc-zc", "系统ID")
	password := flag.String("password", "rXpJe8EA", "密码")
	flag.Parse()

	// 初始化日志
	logger.Init("smpp_client")

	// 连接服务器
	logger.Info(fmt.Sprintf("正在连接到 %s...", *address))
	conn, err := net.Dial("tcp", *address)
	if err != nil {
		logger.Fatal(fmt.Sprintf("连接失败: %v", err))
	}
	defer conn.Close()

	// 发送绑定请求
	logger.Info("发送绑定请求...")
	payload := []byte(*systemID + "\x00" + *password + "\x00" + "smpp_test\x00\x34")
	bindMsg := protocol.NewMessage(protocol.BIND_RECEIVER, 0, 1, payload)
	if _, err := conn.Write(bindMsg.Bytes()); err != nil {
		logger.Fatal(fmt.Sprintf("发送绑定请求失败: %v", err))
	}

	// 读取绑定响应
	headerBuf := make([]byte, 16)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		logger.Fatal(fmt.Sprintf("读取绑定响应失败: %v", err))
	}

	header, err := protocol.ParseHeader(headerBuf)
	if err != nil {
		logger.Fatal(fmt.Sprintf("解析绑定响应头部失败: %v", err))
	}

	if header.CommandID != protocol.BIND_RECEIVER_RESP {
		logger.Fatal(fmt.Sprintf("期望绑定响应，收到命令ID: %d", header.CommandID))
	}

	if header.CommandStatus != protocol.E_SUCCESS {
		logger.Fatal(fmt.Sprintf("绑定失败，状态码: %d", header.CommandStatus))
	}

	logger.Info("绑定成功！")

	// 创建通道接收信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 计数器
	var heartbeatCount uint32
	var messageCount uint32

	// 启动心跳线程
	go func() {
		for {
			time.Sleep(60 * time.Second)

			// 发送链路查询
			seq := atomic.AddUint32(&messageCount, 1)
			enquireMsg := protocol.CreateEnquireLink(seq)

			logger.Info(fmt.Sprintf("发送链路查询 #%d...", atomic.AddUint32(&heartbeatCount, 1)))
			if _, err := conn.Write(enquireMsg.Bytes()); err != nil {
				logger.Error(fmt.Sprintf("发送链路查询失败: %v", err))
				return
			}
		}
	}()

	// 启动消息接收线程
	go func() {
		for {
			// 读取头部
			headerBuf := make([]byte, 16)
			if _, err := io.ReadFull(conn, headerBuf); err != nil {
				if err == io.EOF {
					logger.Error("服务器关闭了连接")
				} else {
					logger.Error(fmt.Sprintf("读取消息头部失败: %v", err))
				}
				return
			}

			header, err := protocol.ParseHeader(headerBuf)
			if err != nil {
				logger.Error(fmt.Sprintf("解析消息头部失败: %v", err))
				continue
			}

			// 读取消息体
			var msg *protocol.Message
			bodyLen := header.CommandLength - 16
			if bodyLen > 0 {
				bodyBuf := make([]byte, bodyLen)
				if _, err := io.ReadFull(conn, bodyBuf); err != nil {
					logger.Error(fmt.Sprintf("读取消息体失败: %v", err))
					continue
				}

				msg, err = protocol.ParseMessage(append(headerBuf, bodyBuf...))
				if err != nil {
					logger.Error(fmt.Sprintf("解析消息失败: %v", err))
					continue
				}
			} else {
				msg, err = protocol.ParseMessage(headerBuf)
				if err != nil {
					logger.Error(fmt.Sprintf("解析无体消息失败: %v", err))
					continue
				}
			}

			// 处理消息
			switch msg.Header.CommandID {
			case protocol.DELIVER_SM:
				logger.Info(fmt.Sprintf("收到DELIVER_SM消息，序列号: %d, 长度: %d",
					msg.Header.SequenceNumber, msg.Header.CommandLength))

				// 解析消息内容
				sourceAddr, destAddr, content, err := parseDeliverSM(msg.Payload)
				if err != nil {
					logger.Error(fmt.Sprintf("解析消息内容失败: %v", err))
				} else {
					logger.Info(fmt.Sprintf("短信内容: 来源=%s, 目标=%s, 内容=%s",
						sourceAddr, destAddr, content))
				}

				// 发送响应
				seq := atomic.AddUint32(&messageCount, 1)
				respMsg := protocol.CreateDeliverSMResponse(seq, protocol.SM_OK)
				if _, err := conn.Write(respMsg.Bytes()); err != nil {
					logger.Error(fmt.Sprintf("发送DELIVER_SM响应失败: %v", err))
				}

			case protocol.ENQUIRE_LINK:
				logger.Info(fmt.Sprintf("收到ENQUIRE_LINK请求，序列号: %d", msg.Header.SequenceNumber))

				// 发送响应
				seq := atomic.AddUint32(&messageCount, 1)
				respMsg := protocol.CreateEnquireLinkResponse(seq)
				if _, err := conn.Write(respMsg.Bytes()); err != nil {
					logger.Error(fmt.Sprintf("发送ENQUIRE_LINK响应失败: %v", err))
				}

			case protocol.ENQUIRE_LINK_RESP:
				logger.Info(fmt.Sprintf("收到ENQUIRE_LINK响应，序列号: %d", msg.Header.SequenceNumber))

			case protocol.UNBIND:
				logger.Info(fmt.Sprintf("收到UNBIND请求，序列号: %d", msg.Header.SequenceNumber))

				// 发送响应
				seq := atomic.AddUint32(&messageCount, 1)
				respMsg := protocol.CreateUnbindResponse(seq, protocol.UNBIND_OK)
				if _, err := conn.Write(respMsg.Bytes()); err != nil {
					logger.Error(fmt.Sprintf("发送UNBIND响应失败: %v", err))
				}

				return

			default:
				logger.Info(fmt.Sprintf("收到未知命令: 0x%08X, 序列号: %d",
					msg.Header.CommandID, msg.Header.SequenceNumber))
			}
		}
	}()

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，正在解绑...")

	// 发送解绑请求
	unbindMsg := protocol.NewMessage(protocol.UNBIND, 0, atomic.AddUint32(&messageCount, 1), nil)
	if _, err := conn.Write(unbindMsg.Bytes()); err != nil {
		logger.Error(fmt.Sprintf("发送解绑请求失败: %v", err))
	}

	time.Sleep(1 * time.Second)
	logger.Info("客户端已关闭")
}

// parseDeliverSM 解析DELIVER_SM消息
func parseDeliverSM(payload []byte) (sourceAddr, destAddr, shortMessage string, err error) {
	fieldsWithNull := make([][]byte, 0)
	start := 0
	for i, b := range payload {
		if b == 0 {
			fieldsWithNull = append(fieldsWithNull, payload[start:i])
			start = i + 1
		}
	}

	if len(fieldsWithNull) < 3 {
		return "", "", "", fmt.Errorf("无效的DELIVER_SM格式")
	}

	if len(fieldsWithNull) > 1 {
		sourceAddr = string(fieldsWithNull[1])
	}

	if len(fieldsWithNull) > 2 {
		destAddr = string(fieldsWithNull[2])
	}

	if len(fieldsWithNull) > 3 {
		shortMessage = string(fieldsWithNull[3])
	}

	return
}
