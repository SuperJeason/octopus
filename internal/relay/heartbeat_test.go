package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/gin-gonic/gin"
)

// newTestGinContext 构造一个测试用的 gin.Context，使用 httptest.ResponseRecorder 收集输出。
// 返回 c, recorder。flush 由 recorder 实现 http.Flusher 协议，可正确处理 writer.Flush()。
func newTestGinContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString("{}"))
	return c, w
}

// 心跳间隔为 0 时返回的是空壳 heartbeat：协程从未启动，所有方法 no-op。
func TestStartEarlyHeartbeat_DisabledByZeroInterval(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "0"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	// Stop 必须不阻塞（done channel 已关闭）
	done := make(chan struct{})
	go func() {
		hb.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop blocked on disabled heartbeat")
	}

	if hb.HeaderWritten() {
		t.Fatal("disabled heartbeat must not write SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("disabled heartbeat wrote bytes: %q", w.Body.String())
	}
}

// 非流式请求（isStream=false）也应当返回空壳，不写 SSE 头。
func TestStartEarlyHeartbeat_DisabledByNonStream(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "60"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, false)
	defer hb.Stop()

	if hb.HeaderWritten() {
		t.Fatal("non-stream heartbeat must not write SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("non-stream heartbeat wrote bytes: %q", w.Body.String())
	}
}

// 流式 + interval>0 时立即写 SSE 响应头与首次心跳。
func TestStartEarlyHeartbeat_ImmediateFirstHeartbeat(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "60"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	if !hb.HeaderWritten() {
		t.Fatal("expected SSE header to be written immediately")
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected Content-Type=text/event-stream, got %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, ":\n\n") {
		t.Fatalf("expected initial heartbeat ':\\n\\n', got %q", body)
	}
	// 必须仅写过一份心跳（ticker 还未触发）
	if count := strings.Count(body, ":\n\n"); count != 1 {
		t.Fatalf("expected exactly 1 initial heartbeat, got %d in %q", count, body)
	}
}

// Hand 后外层心跳协程退出，不再产生新心跳。
func TestEarlyHeartbeat_HandStopsTicker(t *testing.T) {
	setupRelayTestDB(t)
	// 用 1 秒间隔以便快速验证（首心跳立即发出，ticker 1 秒后会再发一次）
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "1"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	// Hand 应同步等待协程退出
	hb.Hand()

	initialCount := strings.Count(w.Body.String(), ":\n\n")
	if initialCount < 1 {
		t.Fatalf("expected at least 1 heartbeat before Hand, got %d", initialCount)
	}

	// Hand 后再过 1.5 秒，不应有新心跳（ticker 已 cancel）
	time.Sleep(1500 * time.Millisecond)
	finalCount := strings.Count(w.Body.String(), ":\n\n")
	if finalCount != initialCount {
		t.Fatalf("expected heartbeat count to remain %d after Hand, got %d", initialCount, finalCount)
	}

	// 二次 Hand / Stop 都应安全 no-op
	hb.Hand()
	hb.Stop()
}

// Stop 在协程未启动时也安全（CompareAndSwap 路径）。
func TestEarlyHeartbeat_StopOnDisabledIsSafe(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "0"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}
	c, _ := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true) // 空壳
	hb.Stop()
	hb.Stop() // 二次 Stop 也必须安全
}

// 已写过 SSE 头时，FlushOrError 发送 SSE error event；未写时走 JSON resp.Error。
func TestEarlyHeartbeat_FlushOrError_SSEPath(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "60"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	hb.FlushOrError(c, http.StatusBadGateway, "channel failed")

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected SSE error event, got %q", body)
	}
	if !strings.Contains(body, `"code":502`) {
		t.Fatalf("expected status code in SSE error payload, got %q", body)
	}
	if !strings.Contains(body, `"message":"channel failed"`) {
		t.Fatalf("expected error message in SSE payload, got %q", body)
	}
	// 不应该再有 application/json 格式的响应
	if strings.Contains(body, `"code":502,"message":"channel failed"}`) && !strings.Contains(body, "event: error") {
		t.Fatalf("FlushOrError fell through to JSON path despite SSE header written: %q", body)
	}
}

func TestEarlyHeartbeat_FlushOrError_JSONPath(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "0"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true) // 空壳
	defer hb.Stop()

	hb.FlushOrError(c, http.StatusBadGateway, "channel failed")

	body := w.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("disabled heartbeat should not produce SSE error event, got %q", body)
	}
	if !strings.Contains(body, `"message":"channel failed"`) {
		t.Fatalf("expected JSON error response, got %q", body)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON Content-Type, got %q", got)
	}
}

// nil heartbeat 的方法均为 no-op，避免调用方判空。
func TestEarlyHeartbeat_NilSafe(t *testing.T) {
	var hb *earlyHeartbeat
	hb.Hand()
	hb.Stop()
	if hb.HeaderWritten() {
		t.Fatal("nil heartbeat must not report header written")
	}
	c, w := newTestGinContext(t)
	hb.FlushOrError(c, http.StatusInternalServerError, "boom")
	// nil heartbeat fall-through 到 resp.Error，应当写 JSON
	body := w.Body.String()
	if !strings.Contains(body, `"message":"boom"`) {
		t.Fatalf("nil heartbeat FlushOrError fallthrough failed: %q", body)
	}
}

// 心跳协程在 ticker 触发时持续发送（验证 select 死锁/竞态）。
func TestEarlyHeartbeat_TickerProducesAdditional(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "1"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)

	// 等待 2.5 秒，期望至少 3 次心跳：初始 1 次 + ticker 2 次
	time.Sleep(2500 * time.Millisecond)
	hb.Stop()

	count := strings.Count(w.Body.String(), ":\n\n")
	if count < 3 {
		t.Fatalf("expected >=3 heartbeats over 2.5s with interval=1s, got %d", count)
	}
}
