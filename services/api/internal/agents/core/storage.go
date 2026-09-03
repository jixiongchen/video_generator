// Package core 是所有 Agent 可复用的基础能力，不包含小说情节或提示词。
// HTTP 接收用户意图，业务 Agent 决定步骤，core 负责把步骤和产物可靠保存。
package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

// ValidID 限制外部传入的项目、产物、任务 ID，不能把 ../ 当文件路径使用。
func ValidID(value string) bool { return validID.MatchString(value) }

func ID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("system randomness unavailable")
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// WriteJSON 先完整写临时文件并同步，再原子替换索引。
// 调用方必须先写产物、后写引用它的索引；失败最多留下可回收的孤立文件，
// 不能让索引指向尚未写完的产物。临时文件必须与目标在同一文件系统。
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, data)
}

func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func ReadJSON(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, target); err != nil {
		return errors.New("本地 Agent 数据损坏，请保留文件并检查备份")
	}
	return nil
}
