package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const defaultAddr = "127.0.0.1:19081"

type config struct {
	addr      string
	dataDir   string
	selfcheck bool
}

func parseConfig(args []string) (config, error) {
	set := flag.NewFlagSet("archive-release", flag.ContinueOnError)
	var cfg config
	set.StringVar(&cfg.addr, "addr", "", "HTTP 监听地址")
	set.StringVar(&cfg.dataDir, "data-dir", ".benzhi/archive-release-data", "本地持久化目录")
	set.BoolVar(&cfg.selfcheck, "selfcheck", false, "启动真实 HTTP 服务并执行完整自检后退出")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("不支持位置参数：%s", strings.Join(set.Args(), " "))
	}
	portValue := strings.TrimSpace(os.Getenv("PORT"))
	if portValue != "" {
		port, err := strconv.Atoi(portValue)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("PORT 必须是 1 到 65535 的端口号")
		}
		fromPort := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		if cfg.addr != "" && cfg.addr != fromPort {
			return config{}, fmt.Errorf("-addr 与 PORT 配置冲突")
		}
		cfg.addr = fromPort
	}
	if cfg.addr == "" {
		cfg.addr = defaultAddr
	}
	host, port, err := net.SplitHostPort(cfg.addr)
	if err != nil {
		return config{}, fmt.Errorf("-addr 必须是 host:port：%w", err)
	}
	parsed := net.ParseIP(host)
	if parsed == nil || !parsed.IsLoopback() {
		return config{}, fmt.Errorf("监听地址必须使用回环 IP")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return config{}, fmt.Errorf("监听端口无效")
	}
	if strings.TrimSpace(cfg.dataDir) == "" {
		return config{}, fmt.Errorf("-data-dir 不能为空")
	}
	return cfg, nil
}
