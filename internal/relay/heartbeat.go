package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

// earlyHeartbeat 管理早期心跳协程，覆盖 Handler 整个生命周期
// （解析 → 选通道 → forward 等待 first-byte → 重试退避 → 失败兜底）。
//
// 现有的心跳 ticker 仅在进入 handleStreamResponse* 之后才启动，但 Handler 中
// 的所有"前置阶段"（特别是多通道 failover 累计耗时）可能轻易超过 120 秒，
// 此时 Cloudflare 看到源站零字节会触发 524。早期心跳协程在 Handler 入口
// 就开始定时发送 SSE 注释，确保即使前置阶段缓慢也有字节流向客户端。
//
// 进入流式响应处理后调用 Hand() 交接给内层 ticker，避免重复 flush 与数据交错。
type earlyHeartbeat struct {
	c        *gin.Context
	interval time.Duration
	enabled  bool

	mu      sync.Mutex // 串行化 Write/Flush，保护 SSE 头与心跳/错误事件互斥
	handed  atomic.Bool
	stopped atomic.Bool
	cancel  context.CancelFunc
	done    chan struct{}

	headerSet atomic.Bool // SSE 响应头是否已发送
}

// startEarlyHeartbeat 在 Handler 入口启动早期心跳。
// 仅当 isStream=true 且 sse_heartbeat_interval>0 时才真正起协程；
// 否则返回一个空壳，所有方法都是 no-op，调用方无需判空。
func startEarlyHeartbeat(c *gin.Context, isStream bool) *earlyHeartbeat {
	h := &earlyHeartbeat{c: c, done: make(chan struct{})}

	interval, err := op.SettingGetInt(dbmodel.SettingKeySSEHeartbeatInterval)
	if err != nil || interval <= 0 || !isStream || c == nil {
		// 空壳：协程从未启动，done 立即关闭，handed/stopped 视为已结束
		close(h.done)
		h.handed.Store(true)
		h.stopped.Store(true)
		return h
	}
	h.enabled = true
	h.interval = time.Duration(interval) * time.Second

	// 立即写 SSE 响应头与第一个心跳，覆盖第 0~interval 秒的盲区
	// （否则 Cloudflare 在第一个 ticker 触发前就可能因零字节而 524）
	h.writeSSEHeaderLocked()
	if err := h.writeHeartbeatLocked(); err != nil {
		log.Debugf("early heartbeat initial write failed: %v", err)
		close(h.done)
		h.handed.Store(true)
		h.stopped.Store(true)
		return h
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	h.cancel = cancel

	safe.Go("relay-early-heartbeat", func() {
		defer close(h.done)
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if h.handed.Load() || h.stopped.Load() {
					return
				}
				h.mu.Lock()
				if h.handed.Load() || h.stopped.Load() {
					h.mu.Unlock()
					return
				}
				if err := h.writeHeartbeatLocked(); err != nil {
					log.Debugf("early heartbeat write failed: %v", err)
					h.mu.Unlock()
					return
				}
				h.mu.Unlock()
			}
		}
	})
	return h
}

// writeSSEHeaderLocked 设置 SSE 响应头并立刻 flush。幂等。
func (h *earlyHeartbeat) writeSSEHeaderLocked() {
	if !h.headerSet.CompareAndSwap(false, true) {
		return
	}
	w := h.c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()
}

// writeHeartbeatLocked 写入 SSE 注释心跳。调用方必须持有 h.mu。
func (h *earlyHeartbeat) writeHeartbeatLocked() error {
	if _, err := h.c.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	h.c.Writer.Flush()
	return nil
}

// Hand 将心跳控制权交给内层 ticker（在进入 handleStreamResponse* 时调用）。
// 调用后外层协程不再发心跳；本方法阻塞至外层协程退出，确保内层 ticker
// 启动时不会与外层 flush 竞争。空壳 heartbeat 上调用是 no-op。
func (h *earlyHeartbeat) Hand() {
	if h == nil {
		return
	}
	if !h.handed.CompareAndSwap(false, true) {
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	<-h.done
}

// Stop 完全停止心跳并阻塞至协程退出（错误兜底路径调用）。
// 与 Hand 的区别：Hand 用于"将控制权交给内层"；Stop 用于"彻底结束"。
// 空壳 heartbeat 上调用是 no-op。
func (h *earlyHeartbeat) Stop() {
	if h == nil {
		return
	}
	h.stopped.Store(true)
	if !h.handed.CompareAndSwap(false, true) {
		// 已经 handed 过，等外层协程结束即可
		<-h.done
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	<-h.done
}

// HeaderWritten 报告 SSE 响应头是否已发送。
// 决定错误兜底路径如何返回：已写头则发 SSE error event，否则走标准 JSON 错误。
func (h *earlyHeartbeat) HeaderWritten() bool {
	if h == nil {
		return false
	}
	return h.headerSet.Load()
}

// WriteSSEError 在已写入 SSE 头的情况下，以 SSE event 形式返回错误，保持协议一致。
// 调用方应先检查 HeaderWritten()。本方法内部加锁，可与外层心跳协程并发安全调用。
func (h *earlyHeartbeat) WriteSSEError(statusCode int, message string) {
	if h == nil || h.c == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"code":    statusCode,
		"message": message,
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = h.c.Writer.Write([]byte("event: error\ndata: "))
	_, _ = h.c.Writer.Write(payload)
	_, _ = h.c.Writer.Write([]byte("\n\n"))
	h.c.Writer.Flush()
}

// FlushOrError 是 resp.Error 的协议感知封装：
// - 若已写过 SSE 头：发送 SSE error event（不再 c.AbortWithStatusJSON，避免协议混合）
// - 否则：走标准 resp.Error JSON 响应
func (h *earlyHeartbeat) FlushOrError(c *gin.Context, statusCode int, message string) {
	if h != nil && h.HeaderWritten() {
		h.WriteSSEError(statusCode, message)
		return
	}
	resp.Error(c, statusCode, message)
}
