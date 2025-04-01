# smps
短信代理平台
smps/
├── cmd/
│   └── server/
│       └── main.go
├── config/
│   └── config.yaml
├── internal/
│   ├── api/
│   ├── auth/
│   ├── client/
│   ├── dispatcher/
│   ├── protocol/
│   └── server/
└── pkg/
    ├── logger/
    └── utils/


运行服务器：go run cmd/server/main.go

访问健康检查API：curl http://localhost:8080/health

查看输出日志确认配置加载成功