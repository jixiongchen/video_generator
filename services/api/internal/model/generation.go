package model

import "time"

type GenerationRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	GenerationMode string `json:"generationMode"`
	ResolutionTier string `json:"resolutionTier"`
	Orientation    string `json:"orientation"`
	Seconds        int    `json:"seconds"`
	Seed           *int64 `json:"seed,omitempty"`
}

type Generation struct {
	ID          string            `json:"id"`
	WorkerJobID string            `json:"workerJobId,omitempty"`
	Request     GenerationRequest `json:"request"`
	Status      string            `json:"status"`
	Progress    int               `json:"progress"`
	VideoURL    string            `json:"videoUrl,omitempty"`
	ProviderID  string            `json:"providerId,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type WorkerJob struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId,omitempty"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	VideoURL   string `json:"videoUrl,omitempty"`
	Error      string `json:"error,omitempty"`
}
