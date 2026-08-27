# Aggregation Cards & Runtime JSON Editor — Design

Date: 2026-08-26
Branch: `development`
Status: approved (pending spec review)

## Overview

Two features delivered together:

1. **Aggregation cards** — admin-defined cards showing a single aggregate value (COUNT / SUM / AVG / MIN / MAX, optional filters) over a table or query view. Shown on a new global Dashboard page and as a compact strip atop the Data grid for the matching table. Aggregate querying is delivered in `kucrud-core` (ds → engine → httpapi), consumed by the platform.
2. **Runtime JSON editor** — text columns that hold mixed content (plain text *or* JSON strings) get JSON-aware display and editing in the grid/form, detected per value at runtime. No schema, migration, or backend changes.

## Feature 1: Aggregation Cards

### 1a. Core — datasource (`core/ds`)

- New params/result types:
  ```go
  type AggregateParams struct {
      Schema, Table string   // physical table mode
      Query         string   // query-view mode (mutually exclusive with Table)
      Func          string   // count | sum | avg | min | max
      Column        string   // required for sum/avg/min/max; empty for count
      Filters       []ColumnFilter
  }
  type AggregateResult struct {
      Value   any  // json.Number-friendly float64/int64/string; nil when NULL
      HasRows bool // true when the underlying set is non-empty
  }
  ```
- New `Adapter` method: `AggregateRows(AggregateParams) (*AggregateResult, error)`, implemented in `postgres.go` and `mysql.go`.
- New sqlkit builder `buildAggregate`:
  - Function names come from an allowlist only (`count|sum|avg|min|max`); `count` renders `COUNT(*)`, others render `AGG(QuoteIdent(col))`. Column names pass the existing identifier regex (`QuoteIdent`).
  - WHERE clause reuses `filterParts` (same filter rendering, FK joins, and injection posture as the grid). No search parameter for v1.
  - Table mode: `SELECT AGG(...) FROM schema.tbl [joins] WHERE ...`.
  - Query-view mode: `SELECT AGG(...) FROM (<query>) ku_q WHERE ...` (follows `buildQueryCount`), executed under the read-only transaction + statement timeout like other query-view reads.
- SQL NULL semantics: SUM/AVG/MIN/MAX over zero rows → NULL (`Value: nil, HasRows: false`); COUNT over zero rows → `0, HasRows: false`.

### 1b. Core — engine (`core/engine`)

- New method on `ReadService`: `Stats(t *defs.Table, q url.Values) (*StatsResult, error)` where `StatsResult{Func, Column, Value any, HasRows bool}`.
- Validation rules (all errors → 400 `STATS_INVALID`):
  - `func` ∈ {count, sum, avg, min, max}.
  - `column` must exist in the def, must not be `m2m` or computed.
  - `count`: takes no column; supplying one is rejected.
  - `sum`/`avg`: column `FieldType` must be `number`.
  - `min`/`max`: column `FieldType` must be `number` or `datetime`.
  - `filters` parsed by the existing `ParseFilters` (same op-per-type matrix, max 10, FK join resolution) — card filters are byte-compatible with grid filters.
- Query-view defs (`SourceType == "query"`): column references resolve against the introspected columns; `AggregateParams.Query` is used.

### 1c. Core — HTTP (`core/httpapi`)

- New anchor `stats`: `GET {base}/stats?func=sum&column=amount&filters=[{"column":"status","op":"eq","values":["paid"]}]`.
- Gated by `OpRead` (same grant as row reads) for both table and query-view defs; query views need no extra guard (read-only by construction).
- Response: `{"func":"sum","column":"amount","value":123.45,"hasRows":true}`; `value` is `null` for NULL aggregates.

### 1d. Platform — persistence & API

- Migration #13 (`internal/meta/meta.go`): table
  `stat_cards(id INTEGER PK, table_def_id INTEGER NOT NULL REFERENCES table_defs(id) ON DELETE CASCADE, label TEXT NOT NULL, func TEXT NOT NULL, column_name TEXT NOT NULL DEFAULT '', filters TEXT NOT NULL DEFAULT '[]', position INTEGER NOT NULL DEFAULT 0)`
  following the `saved_filters` conventions (per-version test file `migration13_test.go`).
- Store `internal/meta/statcards.go`: `ListStatCards`, `SaveStatCard`, `UpdateStatCard`, `DeleteStatCard`, `ReorderStatCards` (position ordered).
- Masked ids via existing `tokenid` with new kind `card`.
- Handlers `internal/api/cards.go` + routes in `server.go`:
  - `GET /api/cards` — any authenticated user. The server filters out cards whose referenced table def the user lacks the `read` grant for (admins see all). Cards carry the masked table-def token; the frontend resolves the data address exactly like `Data.tsx` does (`dataName = def.tableName || tableToken`) when calling `/api/data/{dataName}/stats`.
  - `POST /api/cards`, `PUT /api/cards/{id}`, `DELETE /api/cards/{id}` — `RequireTablesManage` (admins).
  - On save/update: validate `func`/`column`/type rules against the *current* def and run `parseFilters` (defs mutate at runtime; validate at write time, same posture as saved filters).
- When a table def is deleted, cascade removes its cards (FK cascade, no orphan cleanup code).

### 1e. Frontend (`web/`)

- `web/src/lib/types.ts`: `StatCard`, `StatsResult`; api client additions.
- New page `web/src/pages/Dashboard.tsx`, route `/dashboard`, sidebar entry (visible to all authenticated users):
  - Responsive grid of shadcn `Card`s; each card fetches `GET /api/data/{dataName}/stats?...` via `@tanstack/react-query` (query key `["stats", cardId]`, staleTime default).
  - Card shows: label, big formatted value, small func/column caption. NULL aggregates render `—`.
  - Number formatting reuses the aggregate column's `ColumnFormatting.number` config when present (no separate per-card format config in v1).
  - Admin controls: add/edit/delete/reorder cards via dialog.
- Add/edit dialog: table picker (from `/tables` list) → func picker → column picker filtered by the type rules (number for sum/avg; number/datetime for min/max; none for count) → optional filters using the existing `FilterBar` component against the chosen def's columns → label.
- `Data.tsx`: compact card strip above the grid listing cards whose table matches the current def (from the same `/api/cards` list, filtered client-side by masked table token). Same stats endpoint; small sizing. Hidden entirely when no cards match.
- All new UI strings in both `i18n/en.ts` and `i18n/id.ts`.

### 1f. Errors & testing

- Error mapping: invalid func/column/filter → 400 `STATS_INVALID`; datasource failure → 502 `CONN`; query-view timeout → 502 `QUERY_TIMEOUT` (existing code).
- Go tests:
  - `core/ds`: sqlkit `buildAggregate` unit tests (pg + mysql dialects, NULL semantics SQL shape, allowlist rejection).
  - `core/engine`: `Stats` validation matrix (bad func, bad column, wrong types, filter passthrough).
  - `core/httpapi`: anchor dispatch, `OpRead` gate, response shape.
  - `internal/meta`: migration 13 + store CRUD/reorder.
  - `internal/api`: cards handlers RBAC (admin vs reader), read-grant filtering on list, save-time validation.
  - Live pg test for the stats endpoint (pattern of `query_rows_pg_test.go`), including a query-view card.
- Frontend verified via `npm run build` (strict TS) — the repo has no UI test runner.

## Feature 2: Runtime JSON Editor (mixed-content text columns)

Problem: some varchar/text columns hold JSON strings, others plain text — sometimes mixed within one column. Schema-level typing (`FieldType: "json"`) is wrong for mixed columns: server validation would reject legitimate plain-text values.

### Design: per-value detection in the frontend, no backend changes

- Helper `looksLikeJSON(s: string): boolean` — trimmed value starts with `{` or `[` **and** `JSON.parse` succeeds. (Guards against `"123"`, `"true"`, ordinary prose.)
- Grid rendering (`renderValue` in `Data.tsx`): for `text` cells where `looksLikeJSON` is true, render pretty-printed JSON with the existing 3-line clamp + mono styling (reuse the `json` branch). Plain text renders unchanged. Applies automatically to every text column — zero configuration, correct for mixed columns.
- Form editor (`FieldInput`): when the field is a `text` column and the current value `looksLikeJSON`, the textarea shows `prettyJSON(value)` (JSON mode, small mode indicator). Plain values edit as normal text. The raw string is preserved as the source of truth; prettifying happens on render only.
- Save validation (client-side, in the existing submit handler): if the editing session **started** in JSON mode (initial value parsed as JSON), the edited content must still parse as JSON — reuse the `badJson` alert pattern already used for `json` columns. Sessions that started as plain text are unconstrained.
- Server behavior: unchanged. A text column accepts any string; no validation rule is added.

### Accepted trade-offs

- A user cannot turn a JSON value into plain text within one editing session (validation refuses). Rationale: protects against accidental corruption; escape hatch can be added later via an explicit mode toggle if needed.
- Detection is syntactic: a plain-text value that starts with `{` and happens to be valid JSON will be treated as JSON — acceptable, since the treatment (pretty display + parse check) is benign.
- JSON detection runs per rendered cell — negligible cost at page sizes used (≤1000 rows/page), pretty-print already does `JSON.parse` today for `json` columns.

### Testing

- Unit-ish verification through strict TS build (`npm run build`).
- Manual matrix documented in the plan: mixed column values (`{...}`, `[...]`, plain prose, numbers-as-text, empty), grid display, edit session in both modes, save validation both ways.

## Out of scope (v1)

- Group-by / breakdown cards, time-series cards, multi-metric cards.
- Per-card number formatting (uses the column's existing formatting config).
- Card-level search parameter on the stats endpoint.
- Server-side JSON validation for mixed text columns.
- Changing `ColumnListEditor`/FieldType semantics for JSON-over-text (approach A dropped in favor of runtime detection).
