import type { QueryIntrospectResult } from "./types";

export class ApiError extends Error {
  code: string;
  detail: unknown;
  constructor(code: string, message: string, detail: unknown) {
    super(message);
    this.code = code;
    this.detail = detail;
  }
}

export async function api<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (opts.body instanceof FormData) {
    delete headers["Content-Type"]; // let the browser set the multipart boundary
  }
  const r = await fetch(`/api${path}`, { ...opts, headers: { ...headers, ...opts.headers } });
  if (r.status === 401 && !path.startsWith("/auth/")) {
    window.location.href = "/login";
    throw new ApiError("AUTH", "session expired", null);
  }
  if (!r.ok) {
    const e = await r.json().catch(() => ({ code: "INTERNAL", message: r.statusText, detail: null }));
    throw new ApiError(e.code, e.message, e.detail);
  }
  return (r.status === 204 ? null : r.json()) as Promise<T>;
}

export function introspectQuery(dsId: string, query: string) {
  return api<QueryIntrospectResult>(`/datasources/${dsId}/query-introspect`, {
    method: "POST",
    body: JSON.stringify({ query }),
  });
}
