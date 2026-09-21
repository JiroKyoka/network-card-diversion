package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	internal := flag.String("internal-action", "", "internal privileged action")
	targetText := flag.String("targets", defaultTarget, "campus IPv4 addresses or CIDRs")
	stateFile := flag.String("state-file", "", "state file")
	resultFile := flag.String("result-file", "", "result file")
	applyFlag := flag.Bool("apply", false, "apply split routing")
	restoreFlag := flag.Bool("restore", false, "restore routing")
	checkFlag := flag.Bool("check", false, "print routing status")
	flag.Parse()

	if *stateFile == "" {
		var err error
		*stateFile, err = statePath()
		if err != nil {
			fatalResult(err)
		}
	}

	if *internal != "" {
		result := privilegedAction(*internal, *targetText, *stateFile)
		if *resultFile != "" {
			_ = writeJSON(*resultFile, result)
		}
		if !result.OK {
			os.Exit(1)
		}
		return
	}

	if *applyFlag || *restoreFlag {
		action := "apply"
		if *restoreFlag {
			action = "restore"
		}
		result := requestPrivileged(action, *targetText, *stateFile)
		printResult(result)
		if !result.OK {
			os.Exit(1)
		}
		return
	}
	if *checkFlag {
		result := checkStatus(*targetText, *stateFile)
		printResult(result)
		if !result.OK {
			os.Exit(1)
		}
		return
	}

	if err := runWebApp(*stateFile); err != nil {
		fatalResult(err)
	}
}

func fatalResult(err error) {
	fmt.Fprintln(os.Stderr, "错误：", err)
	os.Exit(1)
}

func printResult(result OperationResult) {
	fmt.Println(result.Message)
	for _, line := range result.Details {
		fmt.Println("-", line)
	}
}

func privilegedAction(action, rawTargets, stateFile string) OperationResult {
	if !isAdministrator() {
		return OperationResult{OK: false, Message: "没有获得管理员权限，操作已取消。"}
	}
	switch action {
	case "apply":
		targets, err := parseTargets(rawTargets)
		if err != nil {
			return OperationResult{OK: false, Message: err.Error()}
		}
		return applyRoutes(targets, stateFile)
	case "restore":
		return restoreRoutes(stateFile)
	default:
		return OperationResult{OK: false, Message: "未知操作。"}
	}
}

func requestPrivileged(action, targets, stateFile string) OperationResult {
	tmpDir, err := os.MkdirTemp("", "campus-split-result-")
	if err != nil {
		return OperationResult{Message: "无法创建临时目录：" + err.Error()}
	}
	defer os.RemoveAll(tmpDir)
	resultPath := filepath.Join(tmpDir, "result.json")
	if err := os.WriteFile(resultPath, nil, 0600); err != nil {
		return OperationResult{Message: "无法创建结果文件：" + err.Error()}
	}
	args := []string{"--internal-action", action, "--targets", targets, "--state-file", stateFile, "--result-file", resultPath}
	if err := startElevated(args); err != nil {
		return OperationResult{Message: "未能获得管理员权限：" + err.Error()}
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(resultPath)
		if err == nil && len(data) > 0 {
			var result OperationResult
			if err := json.Unmarshal(data, &result); err != nil {
				return OperationResult{Message: "无法读取操作结果：" + err.Error()}
			}
			return result
		}
		time.Sleep(250 * time.Millisecond)
	}
	return OperationResult{Message: "等待管理员操作超时。"}
}

func checkStatus(rawTargets, stateFile string) OperationResult {
	targets, err := parseTargets(rawTargets)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	state, _ := readState(stateFile)
	snapshot, err := inspectRoutes(targets, state)
	if err != nil {
		return OperationResult{Message: "检查失败：" + err.Error()}
	}
	message := "尚未检测到校园 VPN 全局路由。请先只连接校园 VPN。"
	ok := snapshot.Ready
	if state != nil {
		message = snapshot.Summary
		ok = snapshot.Ready
	}
	return OperationResult{OK: ok, Message: message, Snapshot: &snapshot}
}

type webServer struct {
	stateFile       string
	token           string
	server          *http.Server
	shutdown        func() error
	lifecycleMu     sync.Mutex
	activePages     int
	lifecycleSerial uint64
	exiting         bool
	closeDelay      time.Duration
}

func runWebApp(stateFile string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	token := fmt.Sprintf("%x%x", time.Now().UnixNano(), rand.Uint64())
	app := &webServer{stateFile: stateFile, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.index)
	mux.HandleFunc("/api/"+token+"/status", app.status)
	mux.HandleFunc("/api/"+token+"/apply", app.apply)
	mux.HandleFunc("/api/"+token+"/restore", app.restore)
	mux.HandleFunc("/api/"+token+"/quit", app.quit)
	mux.HandleFunc("/api/"+token+"/lifetime", app.lifetime)
	app.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	app.shutdown = app.server.Close
	url := "http://" + listener.Addr().String() + "/?token=" + token
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = openBrowser(url)
	}()
	log.Printf("Campus Split VPN: %s", url)
	err = app.server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *webServer) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != a.token {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, strings.ReplaceAll(indexHTML, "__TOKEN__", a.token))
}

func (a *webServer) decodeTargets(r *http.Request) string {
	var input struct {
		Targets string `json:"targets"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input)
	if strings.TrimSpace(input.Targets) == "" {
		return defaultTarget
	}
	return input.Targets
}

func (a *webServer) status(w http.ResponseWriter, r *http.Request) {
	a.respond(w, checkStatus(r.URL.Query().Get("targets"), a.stateFile))
}

func (a *webServer) apply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.respond(w, requestPrivileged("apply", a.decodeTargets(r), a.stateFile))
}

func (a *webServer) restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.respond(w, requestPrivileged("restore", defaultTarget, a.stateFile))
}

func (a *webServer) quit(w http.ResponseWriter, r *http.Request) {
	a.respond(w, OperationResult{OK: true, Message: "程序已退出；已经应用的分流路由保持不变。"})
	go func() {
		time.Sleep(100 * time.Millisecond)
		a.requestShutdown()
	}()
}

// lifetime keeps one streaming HTTP request open for each control page. When
// the last browser page disappears, its request context is cancelled and the
// application exits after a short reload grace period. Kernel routes are not
// tied to this process, so an applied split route remains in place.
func (a *webServer) lifetime(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	if !a.pageConnected() {
		http.Error(w, "Application exiting", http.StatusGone)
		return
	}

	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()
	<-r.Context().Done()
	a.pageDisconnected()
}

func (a *webServer) pageConnected() bool {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.exiting {
		return false
	}
	a.activePages++
	a.lifecycleSerial++
	return true
}

func (a *webServer) pageDisconnected() {
	a.lifecycleMu.Lock()
	if a.activePages > 0 {
		a.activePages--
	}
	a.lifecycleSerial++
	serial := a.lifecycleSerial
	shouldWait := a.activePages == 0 && !a.exiting
	a.lifecycleMu.Unlock()
	if !shouldWait {
		return
	}
	delay := a.closeDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	go func() {
		time.Sleep(delay)
		a.lifecycleMu.Lock()
		if a.activePages != 0 || a.lifecycleSerial != serial || a.exiting {
			a.lifecycleMu.Unlock()
			return
		}
		a.exiting = true
		shutdown := a.shutdown
		a.lifecycleMu.Unlock()
		if shutdown != nil {
			_ = shutdown()
		}
	}()
}

func (a *webServer) requestShutdown() {
	a.lifecycleMu.Lock()
	if a.exiting {
		a.lifecycleMu.Unlock()
		return
	}
	a.exiting = true
	shutdown := a.shutdown
	a.lifecycleMu.Unlock()
	if shutdown != nil {
		_ = shutdown()
	}
}

func (a *webServer) respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func platformName() string { return runtime.GOOS }
