import type { ApiError } from "./types";

export class ApiRequestError extends Error {
  status: number;
  body: ApiError;
  constructor(status: number, body: ApiError) {
    super(body.message || body.error || `HTTP ${status}`);
    this.status = status;
    this.body = body;
  }
}

export const API_BASE = "/ui/api";
const BASE = API_BASE;

/** Absolute API URL for links the browser opens directly (exports). */
export function apiUrl(path: string, params: Query = {}): string {
  return BASE + path + qs(params);
}

export type Query = { [k: string]: string | number | boolean | undefined | null | string[] };

export function qs(params: Query): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "" || v === false) continue;
    if (Array.isArray(v)) {
      if (v.length) sp.set(k, v.join(","));
    } else sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

let onUnauthorized: ((e: ApiError) => void) | null = null;
export function setUnauthorizedHandler(fn: (e: ApiError) => void) {
  onUnauthorized = fn;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...(init.headers as Record<string, string>) };
  const method = (init.method || "GET").toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    headers["X-Pyloggle-Request"] = "1";
    if (init.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  }
  const res = await fetch(BASE + path, { ...init, headers, credentials: "same-origin" });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = { error: "bad_response", message: text.slice(0, 200) };
  }
  if (!res.ok) {
    const err = (body as ApiError) || { error: `http_${res.status}` };
    // "No project selected" is not "signed out": the app picks a project instead of signing in again.
    if (res.status === 401 && err.error !== "no_project" && onUnauthorized) onUnauthorized(err);
    throw new ApiRequestError(res.status, err);
  }
  return body as T;
}

export const get = <T,>(path: string, params: object = {}) => api<T>(path + qs(params as Query));
export const post = <T,>(path: string, body?: unknown) => api<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) });
export const del = <T,>(path: string, body?: unknown) => api<T>(path, { method: "DELETE", body: body === undefined ? undefined : JSON.stringify(body) });
