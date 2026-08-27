// Typed access to the kucrud-core HTTP contract. The response shapes are
// the library's engine outputs — this client just mirrors them.

export type ValidationRule = { type: string; param?: number }

export type ColumnDTO = {
  name: string
  label: string
  fieldType: string // text | number | boolean | date | datetime | enum | fk | m2m
  editable: boolean
  required: boolean
  visible: boolean
  searchable: boolean
  sortable: boolean
  enumOptions?: string[]
  validations?: ValidationRule[]
  fk?: { table: string; refColumn: string; displayColumns?: string[] }
}

export type DefDTO = {
  name: string
  label: string
  keyColumns: string[]
  pageSize: number
  columns: ColumnDTO[]
  permissions: { read: boolean; create: boolean; update: boolean; delete: boolean }
}

export type Row = Record<string, unknown>

// rels[col][String(fkValue)] = display row of the referenced record.
export type Rels = Record<string, Record<string, Row>>

export type ListResponse = {
  rows: Row[]
  total: number
  page: number
  pageSize: number
  rels: Rels
}

export type ApiError = { code: string; message: string }

// Row keys travel in URLs as base64url(JSON array of key value strings),
// mirroring the Go implementation in kucrud-core/engine/rowkey.go.
// ["3"] → "WyIzIl0"
export function encodeRowKey(vals: (string | number | boolean | null)[]): string {
  const json = JSON.stringify(vals.map((v) => (v === null ? null : String(v))))
  const bytes = new TextEncoder().encode(json)
  let bin = ""
  bytes.forEach((b) => (bin += String.fromCharCode(b)))
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export class RequestError extends Error {
  status: number
  code: string
  constructor(status: number, err: ApiError) {
    super(err.message)
    this.status = status
    this.code = err.code
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const resp = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!resp.ok) {
    let err: ApiError = { code: "HTTP_" + resp.status, message: resp.statusText }
    try {
      err = (await resp.json()) as ApiError
    } catch {
      // non-JSON error body — keep the fallback
    }
    throw new RequestError(resp.status, err)
  }
  return (await resp.json()) as T
}

export const api = {
  defs: () => request<DefDTO[]>("GET", "/api/defs"),
  rows: (name: string, query = "") =>
    request<ListResponse>("GET", `/api/data/${name}/rows${query}`),
  create: (name: string, values: Row) =>
    request<{ ok: boolean }>("POST", `/api/data/${name}/rows`, values),
  update: (name: string, key: string, values: Row) =>
    request<{ ok: boolean }>("PUT", `/api/data/${name}/rows/${key}`, values),
  remove: (name: string, key: string) =>
    request<{ ok: boolean }>("DELETE", `/api/data/${name}/rows/${key}`),
  fkoptions: (name: string, column: string) =>
    request<{ rows: Row[]; total: number }>("GET", `/api/data/${name}/fkoptions/${column}`),
}
