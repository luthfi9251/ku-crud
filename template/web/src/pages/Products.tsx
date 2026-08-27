import { useCallback, useEffect, useState } from "react"
import { api, ColumnDTO, DefDTO, ListResponse, RequestError, Row, encodeRowKey } from "../api"

const RESOURCE = "products"

// The one CRUD page: columns come from /api/defs, data from
// /api/data/products — the library's response shapes, nothing bespoke.
export default function Products() {
  const [def, setDef] = useState<DefDTO | null>(null)
  const [list, setList] = useState<ListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Row | null>(null)
  const [form, setForm] = useState<Row>({})
  const [fkOptions, setFkOptions] = useState<Record<string, Row[]>>({})

  const showError = useCallback((e: unknown) => {
    setError(
      e instanceof RequestError
        ? `${e.status} ${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : String(e),
    )
  }, [])

  const reload = useCallback(() => {
    api
      .defs()
      .then((defs) => setDef(defs.find((d) => d.name === RESOURCE) ?? null))
      .catch(showError)
    api
      .rows(RESOURCE, `?page=${page}${search ? `&search=${encodeURIComponent(search)}` : ""}`)
      .then((l) => {
        setList(l)
        setError(null)
      })
      .catch(showError)
  }, [page, search, showError])

  useEffect(reload, [reload])

  const columns = (def?.columns ?? []).filter((c) => c.visible && c.fieldType !== "m2m")

  // Fetch the fk picker options once the def is known.
  useEffect(() => {
    if (!def) return
    for (const c of def.columns) {
      if (c.fieldType !== "fk" || !c.fk) continue
      api
        .fkoptions(RESOURCE, c.name)
        .then((o) => setFkOptions((prev) => ({ ...prev, [c.name]: o.rows })))
        .catch(showError)
    }
  }, [def, showError])

  const startCreate = () => {
    setEditing({})
    setForm({})
  }
  const startEdit = (row: Row) => {
    setEditing(row)
    setForm({ ...row })
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      if (editing && Object.keys(editing).length > 0) {
        const key = encodeRowKey(def!.keyColumns.map((k) => editing[k] as string | number))
        await api.update(RESOURCE, key, form)
      } else {
        await api.create(RESOURCE, form)
      }
      setEditing(null)
      reload()
    } catch (err) {
      showError(err)
    }
  }

  const remove = async (row: Row) => {
    try {
      const key = encodeRowKey(def!.keyColumns.map((k) => row[k] as string | number))
      await api.remove(RESOURCE, key)
      reload()
    } catch (err) {
      showError(err)
    }
  }

  const fkLabel = (c: ColumnDTO, row: Row): string => {
    const rel = list?.rels[c.name]?.[String(row[c.name])]
    if (!rel) return row[c.name] === null ? "—" : String(row[c.name])
    return c.fk?.displayColumns?.map((d) => String(rel[d])).join(" ") ?? JSON.stringify(rel)
  }

  const totalPages = list ? Math.max(1, Math.ceil(list.total / list.pageSize)) : 1

  return (
    <section>
      <h1>{def?.label ?? RESOURCE}</h1>

      {error && (
        <p className="error" role="alert">
          {error}
          {error.startsWith("403") && " — wire host auth in authstub/middleware.go"}
        </p>
      )}

      <div className="toolbar">
        <input
          placeholder="Search…"
          value={search}
          onChange={(e) => {
            setPage(1)
            setSearch(e.target.value)
          }}
        />
        <button onClick={startCreate}>New</button>
      </div>

      <table>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.name}>{c.label}</th>
            ))}
            <th />
          </tr>
        </thead>
        <tbody>
          {(list?.rows ?? []).map((row, i) => (
            <tr key={i}>
              {columns.map((c) => (
                <td key={c.name}>
                  {c.fieldType === "fk" ? fkLabel(c, row) : String(row[c.name] ?? "—")}
                </td>
              ))}
              <td className="rowactions">
                <button onClick={() => startEdit(row)}>Edit</button>
                <button onClick={() => remove(row)}>Delete</button>
              </td>
            </tr>
          ))}
          {list && list.rows.length === 0 && (
            <tr>
              <td colSpan={columns.length + 1}>No rows.</td>
            </tr>
          )}
        </tbody>
      </table>

      <div className="toolbar">
        <button disabled={page <= 1} onClick={() => setPage(page - 1)}>
          ←
        </button>
        <span>
          {page} / {totalPages}
        </span>
        <button disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
          →
        </button>
      </div>

      {editing && (
        <form className="card" onSubmit={submit}>
          <h2>{Object.keys(editing).length > 0 ? "Edit" : "New"} {RESOURCE.slice(0, -1)}</h2>
          {def!.columns
            .filter((c) => c.editable && !def!.keyColumns.includes(c.name) && c.fieldType !== "m2m")
            .map((c) => (
              <label key={c.name}>
                {c.label}
                {c.required && " *"}
                {c.fieldType === "fk" ? (
                  <select
                    value={String(form[c.name] ?? "")}
                    onChange={(e) =>
                      setForm({ ...form, [c.name]: e.target.value === "" ? null : Number(e.target.value) })
                    }
                  >
                    <option value="">—</option>
                    {(fkOptions[c.name] ?? []).map((o) => (
                      <option key={String(o.id)} value={String(o.id)}>
                        {String(o.name)}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={c.fieldType === "number" ? "number" : "text"}
                    step={c.fieldType === "number" ? "any" : undefined}
                    required={c.required}
                    value={String(form[c.name] ?? "")}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        [c.name]: c.fieldType === "number" ? Number(e.target.value) : e.target.value,
                      })
                    }
                  />
                )}
              </label>
            ))}
          <div className="toolbar">
            <button type="submit">Save</button>
            <button type="button" onClick={() => setEditing(null)}>
              Cancel
            </button>
          </div>
        </form>
      )}
    </section>
  )
}
