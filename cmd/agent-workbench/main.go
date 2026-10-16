// agent-workbench 启动单项目本地网页工作台。
package main

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"agent-workbench/internal/server"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type options struct {
	Action, Path, Name                               string
	Port                                             int
	NoBrowser                                        bool
	AgentName, Model, Sandbox, Approval, CodexBinary string
	Editor                                           string
}

func parseArgs(args []string) (options, error) {
	var o options
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.Action = args[0]
		args = args[1:]
		if o.Action != "new" && o.Action != "open" && o.Action != "connect" {
			return o, errors.New("命令必须是 new、open 或 connect")
		}
	}
	flags := flag.NewFlagSet("agent-workbench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&o.Name, "name", "", "项目名称")
	flags.IntVar(&o.Port, "port", 0, "监听端口，0 为自动选择")
	flags.BoolVar(&o.NoBrowser, "no-browser", false, "只显示地址")
	flags.StringVar(&o.AgentName, "agent-name", "", "新会话名称")
	flags.StringVar(&o.Model, "model", "", "Codex 模型，默认沿用配置")
	flags.StringVar(&o.Sandbox, "sandbox", "", "read-only 或 workspace-write")
	flags.StringVar(&o.Approval, "approval", "", "on-request 或 never")
	flags.StringVar(&o.CodexBinary, "codex-bin", "", "本地 Codex 可执行文件路径")
	flags.StringVar(&o.Editor, "editor", "", "外部编辑器命令，文件路径追加在末尾")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if o.Port < 0 || o.Port > 65535 {
		return o, errors.New("端口须在 0–65535 范围内")
	}
	if o.Action == "" {
		if flags.NArg() != 0 || o.Name != "" {
			return o, errors.New("选择页不接受目录或名称参数")
		}
	} else {
		if flags.NArg() != 1 {
			return o, errors.New("请提供一个项目目录，选项放在目录前")
		}
		o.Path = flags.Arg(0)
	}
	if o.Action == "new" && strings.TrimSpace(o.Name) == "" {
		return o, errors.New("新建项目需要 --name")
	}
	if o.Action != "new" && o.Name != "" {
		return o, errors.New("--name 只用于新建项目")
	}
	if o.Action != "connect" && (o.AgentName != "" || o.Model != "" || o.Sandbox != "" || o.Approval != "") {
		return o, errors.New("会话选项只用于 connect 命令")
	}
	if o.Sandbox != "" && o.Sandbox != "read-only" && o.Sandbox != "workspace-write" {
		return o, errors.New("沙箱必须为 read-only 或 workspace-write")
	}
	if o.Approval != "" && o.Approval != "on-request" && o.Approval != "never" {
		return o, errors.New("审批策略必须为 on-request 或 never")
	}
	return o, nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage()
			return
		}
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}
func usage() {
	fmt.Println(`Agent Workbench — 本地项目工作台

用法：
  agent-workbench [--port 端口] [--no-browser] [--editor 命令]
  agent-workbench open [--port 端口] [--no-browser] [--editor 命令] <已有目录>
  agent-workbench new --name <名称> [--port 端口] [--no-browser] <新目录>
  agent-workbench connect [--agent-name 名称] [--model 模型] [--sandbox read-only|workspace-write] [--approval on-request|never] [--no-browser] <项目目录>

选项放在目录之前；默认打开浏览器。Ctrl+C 关闭服务。
--editor 指定外部编辑器（如 code），按空格分隔参数；未指定时 macOS 使用默认文本编辑器。`)
}
func run(args []string) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	// 先占用端口，再创建项目，避免端口失败时产生意外目录。
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", o.Port))
	if err != nil {
		return fmt.Errorf("无法启动本地服务：%w", err)
	}
	defer listener.Close()
	var p *project.Project
	switch o.Action {
	case "new":
		p, err = project.Create(o.Path, o.Name)
	case "open", "connect":
		p, err = project.Open(o.Path)
	}
	if err != nil {
		return err
	}
	token := rand.Text()
	handler := server.NewWithCodex(token, p, o.CodexBinary)
	handler.SetExternalEditor(o.Editor)
	defer handler.Close()
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stopped := make(chan error, 1)
	go func() { stopped <- srv.Serve(listener) }()
	if o.Action == "connect" {
		fmt.Println("正在启动 Codex 新会话，交互将交给网页…")
		session, err := handler.CreateAgent(context.Background(), agent.StartOptions{Name: o.AgentName, Model: o.Model, Sandbox: o.Sandbox, ApprovalPolicy: o.Approval})
		if err != nil {
			_ = srv.Close()
			return err
		}
		fmt.Printf("Codex 已连接：%s · %s · %s\n", session.Name, session.ThreadID, session.Model)
	}
	address := "http://" + listener.Addr().String() + "/#token=" + token
	if o.Action == "connect" {
		address += "&view=agents"
	}
	fmt.Println("Agent Workbench 已启动：", address)
	fmt.Println("一个进程绑定一个项目。按 Ctrl+C 关闭服务。")
	if !o.NoBrowser {
		if err := openBrowser(address); err != nil {
			fmt.Fprintln(os.Stderr, "无法自动打开浏览器，请复制上方地址：", err)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case err := <-stopped:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
			return err
		}
	}
	return nil
}
func openBrowser(address string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", address)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	default:
		cmd = exec.Command("xdg-open", address)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
