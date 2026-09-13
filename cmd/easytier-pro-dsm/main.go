// Command easytier-pro-dsm is the Synology DSM client for EasyTier Pro.
//
// It runs in two modes: `supervise` owns the daemon lifecycle (the package
// start-stop script starts it), and `serve` hosts the local API, supervises the
// EasyTier core and performs runtime updates.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"net/http"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/dsmenv"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/runtime"
	"github.com/EasyTier-Pro/easytier-pro-dsm/ui"
)

// buildVersion is injected by the packaging script.
var buildVersion = "dev"

const (
	listenAddress = "127.0.0.1:15890"
	// Respawn policy, mirroring procd's "respawn 3600 5 5".
	respawnWindow = 3600 * time.Second
	respawnDelay  = 5 * time.Second
	respawnLimit  = 5
	killDelay     = 30 * time.Second
	shutdownGrace = 10 * time.Second
)

func main() {
	command := "supervise"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	var err error
	switch command {
	case "supervise":
		err = runSupervise()
	case "serve":
		err = runServe()
	case "version":
		fmt.Printf("easytier-pro-dsm %s\n", buildVersion)
		return
	default:
		err = fmt.Errorf("unknown command %q (expected supervise, serve or version)", command)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "easytier-pro-dsm: %v\n", err)
		os.Exit(1)
	}
}

// runSupervise keeps the serve daemon running and restarts it after crashes.
func runSupervise() error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	logger, err := config.NewLogger(paths.DaemonLogFile(), config.DevMode())
	if err != nil {
		return err
	}
	defer logger.Close()
	writePIDFile(paths, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	windowStart := time.Now()
	restarts := 0
	for {
		command := exec.Command(paths.DaemonBinary(), "serve")
		command.Env = os.Environ()
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			return fmt.Errorf("start %s: %w", paths.DaemonBinary(), err)
		}
		logger.Printf("已启动 easytier-pro-dsm serve（PID %d）", command.Process.Pid)

		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()

		select {
		case <-ctx.Done():
			stopChild(command, logger)
			removePIDFile(paths)
			logger.Printf("已停止 easytier-pro-dsm")
			return nil
		case err := <-exited:
			if ctx.Err() != nil {
				removePIDFile(paths)
				return nil
			}
			logger.Errorf("serve 进程退出: %v", err)
		}

		if time.Since(windowStart) > respawnWindow {
			windowStart = time.Now()
			restarts = 0
		}
		if restarts >= respawnLimit {
			removePIDFile(paths)
			return fmt.Errorf("serve 在 %s 内重启 %d 次，已放弃重启", respawnWindow, restarts)
		}
		restarts++
		logger.Printf("%s 后重启 serve（窗口内第 %d 次）", respawnDelay, restarts)
		select {
		case <-ctx.Done():
			removePIDFile(paths)
			return nil
		case <-time.After(respawnDelay):
		}
	}
}

// runServe hosts the local API and the core supervisor.
func runServe() error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	logger, err := config.NewLogger(paths.DaemonLogFile(), config.DevMode())
	if err != nil {
		return err
	}
	defer logger.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	store := config.NewStore(paths)
	client := console.New(store, logger)
	manager := runtime.NewManager(paths, store, client, logger)
	if err := manager.Start(ctx); err != nil {
		return err
	}
	defer manager.Shutdown()

	auth := dsmenv.New()
	if auth.Bypassed() {
		logger.Printf("警告：已按开发模式跳过 DSM 登录校验")
	}
	server := &http.Server{
		Addr:              listenAddr(),
		Handler:           httpserver.New(manager, client, auth, logger, ui.FS).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// Stop the privileged core first: the supervisor only allows the
		// process a bounded time to exit.
		manager.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Printf("本机 API 监听 %s", listenAddr())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Printf("本机 API 已停止")
	return nil
}

// listenAddr returns the API address. Only development runs may widen it.
func listenAddr() string {
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		return override
	}
	return listenAddress
}

func writePIDFile(paths config.Paths, logger *config.Logger) {
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(paths.PidFile(), pid, 0o600); err != nil {
		logger.Errorf("写入 PID 文件失败: %v", err)
	}
}

func removePIDFile(paths config.Paths) {
	os.Remove(paths.PidFile())
}

// stopChild terminates the serve daemon, escalating to SIGKILL.
func stopChild(command *exec.Cmd, logger *config.Logger) {
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_, _ = command.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(killDelay):
	}
	logger.Errorf("serve 未在 %s 内退出，强制结束", killDelay)
	_ = command.Process.Kill()
	select {
	case <-done:
	case <-time.After(killDelay):
	}
}
