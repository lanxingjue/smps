// cmd/smsc_simulator/main.go
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/protocol"
)

func main() {
	port := flag.String("port", "2775", "监听端口")
	flag.Parse()

	// 监听TCP端口
	addr := fmt.Sprintf(":%s", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("无法监听端口 %s: %v", *port, err)
	}
	defer listener.Close()

	log.Printf("SMSC模拟器正在监听 %s", addr)

	// 处理信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 启动接受连接的goroutine
	connCh := make(chan net.Conn)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("接受连接失败: %v", err)
				continue
			}

			log.Printf("接受来自 %s 的新连接", conn.RemoteAddr())
			connCh <- conn
		}
	}()

	// 主循环
	for {
		select {
		case conn := <-connCh:
			go handleConnection(conn)

		case <-sigCh:
			log.Println("收到信号，正在关闭...")
			return
		}
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		// 读取头部
		headerBuf := make([]byte, 16)
		if _, err := io.ReadFull(conn, headerBuf); err != nil {
			if err == io.EOF {
				log.Printf("客户端断开连接: %s", conn.RemoteAddr())
			} else {
				log.Printf("读取头部失败: %v", err)
			}
			return
		}

		// 解析头部
		header := protocol.SMPPHeader{
			CommandLength:  binary.BigEndian.Uint32(headerBuf[0:4]),
			CommandID:      binary.BigEndian.Uint32(headerBuf[4:8]),
			CommandStatus:  binary.BigEndian.Uint32(headerBuf[8:12]),
			SequenceNumber: binary.BigEndian.Uint32(headerBuf[12:16]),
		}

		// 读取消息体
		bodyLen := header.CommandLength - 16
		var bodyBuf []byte
		if bodyLen > 0 {
			bodyBuf = make([]byte, bodyLen)
			if _, err := io.ReadFull(conn, bodyBuf); err != nil {
				log.Printf("读取消息体失败: %v", err)
				return
			}
		}

		// 处理不同类型的消息
		switch header.CommandID {
		// 在收到BIND_RECEIVER后发送绑定响应
		case protocol.BIND_RECEIVER:
			log.Printf("收到BIND_RECEIVER请求，序列号: %d", header.SequenceNumber)

			// 验证身份信息(这里只是模拟，实际不验证)
			systemID := ""
			for i, b := range bodyBuf {
				if b == 0 {
					systemID = string(bodyBuf[:i])
					break
				}
			}

			log.Printf("客户端系统ID: %s", systemID)

			// 发送绑定响应
			resp := protocol.CreateBindResponse(header.SequenceNumber, protocol.SMPP_ESME_ROK, "SMSC_SIM")
			conn.Write(resp.Bytes())

			// 延迟发送DELIVER_SM消息
			go func() {
				time.Sleep(2 * time.Second)
				// 构造短信内容
				sms := fmt.Sprintf("This is a test message %d", header.SequenceNumber)
				sourceAddr := "10086"
				destAddr := "13800138000"

				// 创建DELIVER_SM消息
				payload := []byte(sourceAddr + "\x00" + destAddr + "\x00" + sms + "\x00")
				msg := protocol.NewMessage(protocol.DELIVER_SM, 0, header.SequenceNumber+1000, payload)

				log.Printf("发送DELIVER_SM，序列号: %d", header.SequenceNumber+1000)
				if _, err := conn.Write(msg.Bytes()); err != nil {
					log.Printf("发送DELIVER_SM失败: %v", err)
				} else {
					log.Printf("成功发送DELIVER_SM消息")
				}
			}()

		case protocol.ENQUIRE_LINK:
			log.Printf("收到ENQUIRE_LINK请求，序列号: %d", header.SequenceNumber)

			// 发送链路查询响应
			resp := protocol.CreateEnquireLinkResponse(header.SequenceNumber)
			conn.Write(resp.Bytes())

		case protocol.DELIVER_SM_RESP:
			log.Printf("收到DELIVER_SM_RESP，序列号: %d, 状态: %d",
				header.SequenceNumber, header.CommandStatus)

		default:
			log.Printf("收到未知命令ID: %d，序列号: %d",
				header.CommandID, header.SequenceNumber)
		}

		// 定期发送DELIVER_SM消息模拟短信
		if header.CommandID == protocol.BIND_RECEIVER_RESP || header.CommandID == protocol.ENQUIRE_LINK {
			go func(seqNum uint32) {
				time.Sleep(2 * time.Second)

				// 构造短信内容
				sms := fmt.Sprintf("This is a test message %d", seqNum)
				sourceAddr := "10086"
				destAddr := "13800138000"

				// 创建DELIVER_SM消息
				payload := []byte(sourceAddr + "\x00" + destAddr + "\x00" + sms + "\x00")
				msg := protocol.NewMessage(protocol.DELIVER_SM, 0, seqNum+1000, payload)

				log.Printf("发送DELIVER_SM，序列号: %d", seqNum+1000)
				conn.Write(msg.Bytes())
			}(header.SequenceNumber)
		}
	}
}
