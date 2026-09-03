// Package novel 管理小说改编业务。此包不实现供应商 SDK，也不混入视频任务。
package novel

import "encoding/json"

type Chapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type Source struct {
	ID        string `json:"id"`
	ChapterID string `json:"chapterId"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
}

// DocumentRef 同时保留草稿版本和最后一次确认版本，重写不覆盖确认稿。
type DocumentRef struct {
	Current  int  `json:"current"`
	Approved int  `json:"approved"`
	Stale    bool `json:"stale"`
}

type Project struct {
	ID                string                 `json:"id"`
	Title             string                 `json:"title"`
	Revision          int                    `json:"revision"`
	CharacterCount    int                    `json:"characterCount"`
	Encoding          string                 `json:"encoding"`
	Chapters          []Chapter              `json:"chapters"`
	Sources           []Source               `json:"sources"`
	ChaptersConfirmed bool                   `json:"chaptersConfirmed"`
	TargetSeconds     int                    `json:"targetSeconds"`
	TargetEpisodes    int                    `json:"targetEpisodes"`
	Documents         map[string]DocumentRef `json:"documents"`
	RunIDs            []string               `json:"runIds"`
	Warnings          []string               `json:"warnings"`
	CreatedAt         string                 `json:"createdAt"`
	UpdatedAt         string                 `json:"updatedAt"`
}

type Document struct {
	ID        string          `json:"id"`
	Revision  int             `json:"revision"`
	Content   json.RawMessage `json:"content"`
	Origin    string          `json:"origin"`
	CreatedAt string          `json:"createdAt"`
}

type Episode struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Goal             string   `json:"goal"`
	Conflict         string   `json:"conflict"`
	Summary          string   `json:"summary"`
	Hook             string   `json:"hook"`
	Bridge           string   `json:"bridge"`
	EstimatedSeconds int      `json:"estimatedSeconds"`
	SourceIDs        []string `json:"sourceIds"`
	Changes          []string `json:"changes"`
}

type Outline struct {
	Episodes []Episode `json:"episodes"`
}
