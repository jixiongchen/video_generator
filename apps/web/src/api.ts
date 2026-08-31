import type { AppConfig, Generation, GenerationRequest } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers
    }
  });
  const payload = (await response.json()) as T & {
    error?: string | { message?: string };
  };
  if (!response.ok) {
    const message =
      typeof payload.error === "string" ? payload.error : payload.error?.message;
    throw new Error(message ?? `请求失败 (${response.status})`);
  }
  return payload;
}

export const api = {
  config: () => request<AppConfig>("/api/v1/config"),
  listGenerations: async () => {
    const response = await request<{ items: Generation[] }>("/api/v1/generations");
    return response.items;
  },
  createGeneration: (input: GenerationRequest) =>
    request<Generation>("/api/v1/generations", {
      method: "POST",
      body: JSON.stringify(input)
    }),
  cancelGeneration: (id: string) =>
    request<Generation>(`/api/v1/generations/${id}/cancel`, {
      method: "POST",
      body: "{}"
    }),
  generationVideoUrl: (id: string, download = false) =>
    `/api/v1/generations/${encodeURIComponent(id)}/video${download ? "?download=1" : ""}`
};
