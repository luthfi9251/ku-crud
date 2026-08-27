# 04 — HTTP API

Every registered definition serves the same anchor-relative route family. The handler
finds its route root (`rows`, `fkoptions`, `m2moptions`, `import`, `stats`) wherever it
appears in the URL path, so the same handler works under any mount prefix (a prefix
must not itself contain a segment named like an anchor).

Conventions:

- All bodies and responses are JSON (`Content-Type: application/json`) except CSV
  export.
- All errors are `{"code": "<CODE>", "message": "<human text>", "detail": <any>}` with
  a matching HTTP status. `detail` carries structured data where useful (drift
  reports, bulk failures, conflict sources).
- Row keys in URLs (`{key}` below) are opaque: `base64url(JSON array of key value
  strings)` — e.g. `["3"]` → `WyIzIl0`, composite `["7","x"]` → `WyI3IiwieCJd`. Never
  build them by hand; take them from the `rows` response or `/api/defs`-adjacent
  tooling (`engine.EncodeRowKey`).

## Route table

| Route | Method | Gate Op | Write-guarded | Purpose |
|---|---|---|---|---|
| `/rows` | GET | `OpRead` | — | List page |
| `/rows` | POST | `OpCreate` | yes | Insert |
| `/rows/export` | GET | `OpExport` | — | CSV export |
| `/rows/bulk-delete` | POST | `OpDelete` | yes | Bulk delete |
| `/rows/{key}` | GET | `OpRead` | — | Single row |
| `/rows/{key}` | PUT | `OpUpdate` | yes | Update |
| `/rows/{key}` | DELETE | `OpDelete` | yes | Delete |
| `/rows/{key}/m2m/{column}` | GET | `OpRead` | yes* | M2M link values |
| `/fkoptions/{column}` | GET | `OpRead` | yes* | FK picker page |
| `/m2moptions/{column}` | GET | `OpRead` | yes* | M2M picker page |
| `/import/preview` | POST | `OpImport` | yes | CSV import preview |
| `/import/apply` | POST | `OpImport` | yes | CSV import apply |
| `/stats` | GET | `OpRead` | — | Single-value aggregate |

\* "write-guarded" here means the query-view guard rejects the route outright for
`SourceType: "query"` defs (they cannot declare relations); for tables it is a normal
read. The write guard runs **before** the gate; method mismatches get `405` with an
`Allow` header; unknown sub-paths get `404 NOT_FOUND`.

On the App mux only: `GET /api/defs` lists registered definitions with per-caller
permissions (see below).

## List — `GET /rows`

Query parameters:

| Param | Meaning |
|---|---|
| `page` | 1-based page number; page size comes from the def (`PageSize`, default 20, max 200) |
| `search` | substring search across the def's `searchable` columns (`ILIKE`/`LIKE`, `%`/`_` escaped) |
| `sort` / `dir` | any sortable column + `ASC`/`DESC`; default from the def, fallback first key column |
| `filters` | JSON array, see below (max 10 entries) |

```console
$ curl -s '.../rows?search=acme&sort=amount&dir=DESC&page=2&filters='\
'[{"column":"status","op":"eq","values":["paid"]}, {"column":"amount","op":"between","values":["100","500"]}]'
```

Response:

```json
{
  "rows":   [ {"id": 12, "customer_id": 3, "amount": 250.0, "status": "paid", "tags": null} ],
  "total":  87,
  "page":   2,
  "pageSize": 20,
  "rels":     { "customer_id": { "3": {"id": 3, "name": "Acme"} } },
  "m2mRels":  { "tags": { "12": [{"id": 1, "label": "priority"}, {"id": 4, "label": "q3"}] } }
}
```

- `rels`/`m2mRels` are keyed by column name, then by the row's value rendered as a
  string. They are **enrichment** — absent entries never change row data, and they are
  skipped entirely when a read-grant callback denies the target.
- Query views: identical shape, `rels`/`m2mRels` always empty; requires at least one
  sortable column (else `400 VALIDATION`).

### Filter operators by type

| Column type | Operators | Notes |
|---|---|---|
| text, uuid | `eq neq contains in` | values are strings |
| number | `eq neq gt gte lt lte between in` | values parse as numbers |
| datetime | `eq gt lt between` | RFC3339 or `2006-01-02[THH:MM]`; a date-only *upper* bound gets ` 23:59:59` appended (whole-day inclusive) |
| boolean | `eq` | `true`/`false` |
| enum | `eq neq in` | value must be in options |
| fk | `contains eq` | matched against the **target's display columns** (LEFT JOIN), not the raw ref value |
| json, m2m, computed | — | not filterable |

`between` takes exactly 2 values; `in` takes 1..50. Filters AND-combine. Unknown
columns/ops/values → `400 FILTER_INVALID`.

## Single row — `GET /rows/{key}`

```json
{ "row": {"id": 12, "customer_id": 3, "amount": 250.0, "status": "paid"},
  "rels": {"customer_id": {"3": {"id": 3, "name": "Acme"}}} }
```

404 `NOT_FOUND` when absent. Query views without key columns reject with `400
QUERY_NO_KEY`.

## Insert — `POST /rows`

Body: a JSON object of **editable** columns (plus key columns on insert). Unknown or
non-editable keys are rejected (`400 VALIDATION`); required columns enforced; per-type
checks then per-column validation rules run; `json` columns accept strings or
embedded objects/arrays (stored compacted).

```console
$ curl -s -X POST .../rows -d '{"name":"Widget","price":9.99,"tags":[1,4]}'
{"ok":true}
```

m2m columns take arrays of target ref values; links are synced inside the write
(junction create+delete grants permitting). fk columns are existence-checked — a
dangling reference is `400 VALIDATION` ("referenced row not found"); database FK
violations surface as `409 CONFLICT`. Before-hooks run *before* final validation and
may rewrite the payload (see [06](06-hooks-and-actions.md)).

## Update / Delete — `PUT|DELETE /rows/{key}`

`PUT` body: the editable columns to change (partial). The pre-write row is fetched
first (404 when absent) and handed to before-hooks as `Old`.

```json
{"ok": true, "affected": 1}
```

`DELETE` first runs **inbound-reference protection** when the host supplies
`RefSources`: rows referenced by other defined tables' fk columns cannot be deleted —

```json
{"code":"CONFLICT","message":"row is referenced by other tables",
 "detail":[{"table":"order_items","column":"product_id","count":3}]}
```

— then before-hooks, the delete, and after-hooks carrying the old row.

## Bulk delete — `POST /rows/bulk-delete`

Body `{"keys": ["WyIzIl0", ...]}` — max 1000 (deduplicated), partial success by design:

```json
{"deleted": 8, "failed": 2,
 "failures": [{"key": "WyI3Il0", "code": "CONFLICT", "message": "...", "detail": null}]}
```

## Export — `GET /rows/export`

Accepts the same query params as list (`search`, `filters`, `sort`, `dir` — no
pagination). Streams `text/csv` with a UTF-8 BOM and
`Content-Disposition: attachment; filename="<table>-<timestamp>.csv"`. Header row =
visible column labels; fk cells render as display columns joined with `" — "`, m2m
cells as comma-joined display strings. Cap: 100 000 rows (`400 EXPORT_TOO_LARGE`).

## Stats — `GET /stats`

One single-value aggregate (dashboard cards):

| Param | Meaning |
|---|---|
| `func` | `count` \| `sum` \| `avg` \| `min` \| `max` |
| `column` | required for sum/avg (must be `number`), min/max (`number` or `datetime`); **must be omitted** for count |
| `filters` | same filter format as list |

```json
{"func":"sum","column":"amount","value":12345.5,"hasRows":true}
```

SQL NULL semantics preserved: sum/avg/min/max over an empty set → `"value": null,
"hasRows": false`; count → `0, false`. Numeric aggregates arriving as strings on some
drivers are parsed to JSON numbers; datetime min/max stay strings.

## Relation endpoints

- `GET /fkoptions/{column}` — paginated, searchable picker over the fk target
  (`page`, `search`): `{"rows":[{"id":3,"name":"Acme"}],"total":1,"page":1,"pageSize":20}`.
  Sorted by the target's own default sort. 404 when the column isn't fk; 403 when the
  host's `CanRead` denies the target.
- `GET /m2moptions/{column}` — same shape over the m2m target (requires read on both
  junction and target).
- `GET /rows/{key}/m2m/{column}` — the row's current links:
  `{"values": [1, 4], "rows": [{"id":1,"label":"priority"},{"id":4,"label":"q3"}]}`.

## CSV import — `POST /import/preview`, `POST /import/apply`

Multipart form: `file` (≤ 5 MB, ≤ 10 000 data rows), optional `mapping` (JSON object
header→columnName; **required** for apply), apply-only `mode` (`"valid"` skip invalid
rows — the default — or `"all"` best-effort). Delimiters (comma / semicolon / tab)
are sniffed from the first line; headers auto-map by exact name, lowercased name, then
lowercased label.

Preview:

```json
{"delimiter": ",", "headers": ["name", "price"],
 "mapping": {"name": "name", "price": "price"},
 "counts": {"total": 2, "valid": 1, "invalid": 1},
 "rows": [ {"values": {"name": "A", "price": "9.99"}, "valid": true, "errors": []},
           {"values": {"name": "", "price": "x"}, "valid": false,
            "errors": [{"column": "name", "message": "name is required"},
                       {"column": "price", "message": "not a number"}]} ]}
```

Preview runs before-hooks so rejections surface identically to a real insert, and
batch-verifies fk values (one `IN` query per fk column). Apply:

```json
{"inserted": 1, "failed": 1,
 "failures": [{"row": 1, "errors": [{"column": "price", "message": "..."}]}]}
```

Hooks run exactly once per row at insert time.

## Definitions listing — `GET /api/defs`

App-mux route. Lists registered defs in registration order:

```json
[{
  "name": "products", "label": "products", "schema": "public", "table": "products",
  "keyColumns": ["id"], "pageSize": 20,
  "defaultSortCol": "created_at", "defaultSortDir": "DESC",
  "columns": [
    {"name": "id", "label": "Id", "fieldType": "number", "editable": false,
     "required": true, "visible": true, "searchable": true, "sortable": true, "position": 0},
    {"name": "category_id", "label": "Category", "fieldType": "fk", "baseType": "number",
     "fk": {"table": "categories", "refColumn": "id", "displayColumns": ["name"]}, "...": "..."},
    {"name": "tags", "fieldType": "m2m",
     "m2m": {"junctionTable": "product_tags", "srcCol": "product_id", "tgtCol": "tag_id",
             "displayColumns": ["label"]},
     "m2mRefColumn": "id", "m2mTargetRef": "id"},
    {"name": "total", "fieldType": "number", "isComputed": true, "computedFormula": "price * qty"}
  ],
  "permissions": {"read": true, "create": true, "update": true, "delete": true}
}]
```

With a `Gate` installed the listing probes it per def for each op class, so the
response reflects the caller's access; without a gate all permissions are true. Query
views force `create/update/delete` to false.

## Error codes

| Code | Status | Raised by |
|---|---|---|
| `VALIDATION` | 400 | bad body, type/rule violations, unknown columns, missing query-sort |
| `FILTER_INVALID` | 400 | bad filters JSON/ops/values |
| `STATS_INVALID` | 400 | bad aggregate func/column/type combination |
| `HOOK_MISSING` / `HOOK_REJECTED` | 400 | assignment names absent from the binary / guard rejection |
| `EXPORT_TOO_LARGE` | 400 | export over 100 000 rows |
| `BULK_TOO_LARGE` | 400 | bulk-delete over 1000 keys |
| `IMPORT_BAD_CSV` | 400 | undecodable CSV |
| `QUERY_NO_KEY` | 400 | single-row GET on a keyless query view |
| `FORBIDDEN` | 403 | Gate rejection (message from the gate error) |
| `QUERY_READONLY` | 403 | any write op on a query view |
| `NOT_FOUND` | 404 | unknown route/row/token |
| `METHOD_NOT_ALLOWED` | 405 | wrong verb (response carries `Allow`) |
| `CONFLICT` | 409 | inbound fk references on delete; DB FK violations |
| `INTERNAL` | 500 | unexpected failure |
| `CONN` | 502 | datasource/query failure |
| `QUERY_TIMEOUT` | 502 | query-view statement exceeded `ds.QueryTimeout` |
