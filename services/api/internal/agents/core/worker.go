package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Executor 是 Go 与 Python 的边界。测试可注入无网络实现，不需要付费 Key。
// Python 返回内容，Go 才能落盘；Python 不直接修改项目和运行状态。
type Executor interface {
	Call(context.Context, string, any, any) error
}

type Worker struct {
	URL  string
	HTTP *http.Client
}

func NewWorker(url string) *Worker {
	// 文本步骤可持续数分钟，不能复用视频轮询的 15 秒客户端。
	return &Worker{URL: url, HTTP: &http.Client{Timeout: 20 * time.Minute}}
}

func (c *Worker) Call(ctx context.Context, path string, input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	method := http.MethodPost
	if input == nil {
		method = http.MethodGet
	}
	r, err := http.NewRequestWithContext(ctx, method, c.URL+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("无法创建 Agent Worker 请求")
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return errors.New("Agent Worker 连接中断或超时；请确认 Python 服务，未完成调用可能已经计费")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return errors.New("Agent Worker 返回错误")
	}
	return json.NewDecoder(resp.Body).Decode(output)
}
