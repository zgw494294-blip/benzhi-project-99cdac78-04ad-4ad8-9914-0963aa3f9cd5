package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/auditlog"
	"oral-archive-release/internal/filestore"
	"oral-archive-release/internal/httpapi"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("archive-release 退出：%v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	repo, err := filestore.Open(filepath.Join(cfg.dataDir, "store"))
	if err != nil {
		return fmt.Errorf("恢复案件存储: %w", err)
	}
	audit, err := auditlog.Open(filepath.Join(cfg.dataDir, "audit"))
	if err != nil {
		return fmt.Errorf("恢复审计存储: %w", err)
	}
	service := application.NewService(repo, audit)
	handler := httpapi.NewHandler(service)
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.addr, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	log.Printf("口述录音开放审查服务监听 %s", cfg.addr)
	if cfg.selfcheck {
		checkErr := runSelfcheck(context.Background(), "http://"+listener.Addr().String())
		shutdownErr := boundedShutdown(server)
		serverErr := <-serveErr
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			return serverErr
		}
		if checkErr != nil {
			return fmt.Errorf("selfcheck 失败: %w", checkErr)
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		log.Printf("selfcheck 通过：完整开放流程、凭据和审计链均已核验")
		return nil
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signalCtx.Done():
		if err := boundedShutdown(server); err != nil {
			return err
		}
		serverErr := <-serveErr
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			return serverErr
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func boundedShutdown(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("关闭 HTTP 服务: %w", err)
	}
	return nil
}
