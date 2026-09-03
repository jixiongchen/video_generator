package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExternalPageCannotStartLocalTask(t *testing.T) {
	called := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/novels/x/agent-runs", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if called || w.Code != http.StatusForbidden {
		t.Fatal("外部网页请求已进入业务处理")
	}
	r.Header.Set("Origin", "http://127.0.0.1:5174")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if !called || w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5174" {
		t.Fatal("本机 Vite 端口被错误阻止")
	}
}
