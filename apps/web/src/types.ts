export type GenerationStatus =
  | "pending"
  | "queued"
  | "running"
  | "waiting_provider"
  | "succeeded"
  | "failed"
  | "canceled"
  | "stale";

export interface GenerationRequest {
  model: string;
  prompt: string;
  generationMode: "t2v";
  resolutionTier: "480p" | "720p" | "1080p";
  orientation: "landscape" | "portrait" | "square";
  seconds: 5 | 10;
  seed?: number;
}

export interface Generation {
  id: string;
  providerId?: string;
  request: GenerationRequest;
  status: GenerationStatus;
  progress: number;
  videoUrl?: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AppConfig {
  defaults: GenerationRequest;
  capabilities: {
    generationModes: string[];
    resolutions: GenerationRequest["resolutionTier"][];
    orientations: GenerationRequest["orientation"][];
    seconds: GenerationRequest["seconds"][];
  };
}

