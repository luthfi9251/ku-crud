import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { api } from "../../lib/api";
import { encodeRowKey } from "../../lib/rowkey";
import type { ColumnDef, Row, RowsRes, TableDefPayload } from "../../lib/types";
import { formatCell, enumColorClass } from "../../lib/format";
import { Badge } from "@/components/ui/badge";

const COL_CAP = 2000; // per-column safety cap (infinite scroll keeps going to a bound)

interface ColState { rows: Row[]; page: number; total: number; loading: boolean }

// Kanban board over an enum column. Cards are dragged between columns with
// native HTML5 drag & drop (no dependency). Each column lazy-loads its own
// pages. NULL board values cannot be selected with the eq filter, so the
// "No value" column fetches whole pages (global filters only) and filters
// NULLs client-side.
export function KanbanView({ def, boardCol, displayCol, search, filters, pageSize, lang, dataVersion, onEdit, onDelete, onCreate }: {
  def: TableDefPayload;
  boardCol: ColumnDef;
  displayCol?: ColumnDef;
  search: string;
  filters: string;
  pageSize: number;
  lang: string;
  dataVersion?: number;
  onEdit: (row: Row) => void;
  onDelete: (key: string[]) => void;
  onCreate: () => void;
}) {
  const values = useMemo(() => boardCol.enumOptions ?? [], [boardCol]);
  const [perCol, setPerCol] = useState<Record<string, ColState>>(() =>
    Object.fromEntries([...values, ""].map((v) => [v, { rows: [], page: 0, total: 0, loading: false }]))
  );
  const [drag, setDrag] = useState<{ row: Row; from: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const keyOf = (row: Row) => def.keyColumns.map((k) => row[k]);

  // one page of rows for a column value; the "No value" column filters NULLs client-side
  const fetchPage = useCallback(async (value: string, page: number) => {
    const filterVal = value === "" ? null : value;
    const fs = filterVal !== null
      ? JSON.stringify([{ column: boardCol.name, op: "eq", values: [filterVal] }])
      : filters;
    const q = new URLSearchParams();
    if (fs) q.set("filters", fs);
    if (search) q.set("search", search);
    q.set("page", String(page));
    q.set("limit", String(pageSize));
    const res = await api<RowsRes>(`/tables/${def.id}/rows?${q}`);
    const rows = value === ""
      ? res.rows.filter((r) => r[boardCol.name] === null || r[boardCol.name] === undefined)
      : res.rows;
    return { rows, total: res.total };
  }, [boardCol, filters, search, pageSize, def.id]);

  const loadMore = useCallback(async (value: string) => {
    const st = perCol[value];
    if (!st || st.loading) return;
    if (st.rows.length >= COL_CAP || (st.total > 0 && st.rows.length >= st.total)) return;
    setPerCol((p) => ({ ...p, [value]: { ...p[value], loading: true } }));
    try {
      const { rows, total } = await fetchPage(value, st.page + 1);
      setPerCol((p) => ({
        ...p,
        [value]: { rows: [...p[value].rows, ...rows], page: p[value].page + 1, total, loading: false },
      }));
    } catch {
      setPerCol((p) => ({ ...p, [value]: { ...p[value], loading: false } }));
    }
  }, [perCol, fetchPage]);

  // reset one column and re-fetch its first page — used when filters/search
  // change and to re-sync after a failed drop (loadMore appends and would
  // duplicate rows)
  const refreshCol = useCallback(async (value: string) => {
    setPerCol((p) => ({ ...p, [value]: { rows: [], page: 0, total: 0, loading: true } }));
    try {
      const { rows, total } = await fetchPage(value, 1);
      setPerCol((p) => ({ ...p, [value]: { rows, page: 1, total, loading: false } }));
    } catch {
      setPerCol((p) => ({
        ...p,
        [value]: { rows: p[value]?.rows ?? [], page: p[value]?.page ?? 0, total: p[value]?.total ?? 0, loading: false },
      }));
    }
  }, [fetchPage]);

  // (re)load every column's first page when the board values, filters,
  // search or row data change; deferred a tick so the effect never calls
  // setState synchronously
  useEffect(() => {
    const t = setTimeout(() => { for (const v of [...values, ""]) refreshCol(v); }, 0);
    return () => clearTimeout(t);
  }, [values, refreshCol, dataVersion]);

  const drop = async (row: Row, target: string) => {
    const key = keyOf(row);
    if (key.some((v) => v === null || v === undefined)) return;
    const keyStr = encodeRowKey(key as string[]);
    const newVal = target === "" ? null : target;
    if (String(row[boardCol.name] ?? "") === String(newVal ?? "")) return;
    const from = String(row[boardCol.name] ?? "");
    const to = String(newVal ?? "");
    setBusy(keyStr);
    try {
      await api(`/tables/${def.id}/rows/${keyStr}`, {
        method: "PUT", body: JSON.stringify({ [boardCol.name]: newVal }),
      });
      // move the card locally — the server accepted the update; a plain
      // loadMore would append a duplicate row to the target column
      setPerCol((p) => {
        const next = { ...p };
        for (const v of Object.keys(next)) {
          next[v] = { ...next[v], rows: next[v].rows.filter((r) => keyOf(r).join("|") !== key.join("|")) };
        }
        if (next[from]) next[from] = { ...next[from], total: Math.max(0, next[from].total - 1) };
        if (next[to]) next[to] = { ...next[to], rows: [{ ...row, [boardCol.name]: newVal }, ...next[to].rows], total: next[to].total + 1 };
        return next;
      });
    } catch (e) {
      alert(e instanceof Error ? e.message : "update failed");
      // re-sync source and target columns from server truth
      await Promise.all([refreshCol(from), refreshCol(to)]);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="flex gap-3 overflow-x-auto pb-4">
      {[...values, ""].map((v) => {
        const st = perCol[v] ?? { rows: [], page: 0, total: 0, loading: false };
        return (
          <div key={v || "__null"} className="flex w-64 shrink-0 flex-col rounded-lg border bg-muted/20">
            <div className="flex items-center justify-between border-b px-3 py-2">
              <span className="flex items-center gap-1.5 text-xs font-semibold">
                {v === "" ? <span className="italic text-muted-foreground">No value</span> : (
                  <Badge variant="outline" className={`text-[10px] ${enumColorClass(boardCol, v)}`}>{v}</Badge>
                )}
                <span className="text-muted-foreground">({st.rows.length})</span>
              </span>
              {v !== "" && (
                <button onClick={() => onCreate()} title="Add record" className="text-muted-foreground hover:text-foreground">
                  <Plus className="h-3 w-3" />
                </button>
              )}
            </div>
            <div
              className="flex-1 space-y-2 overflow-y-auto p-2"
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => { e.preventDefault(); if (drag) drop(drag.row, v); setDrag(null); }}
            >
              {st.rows.map((row) => {
                const key = keyOf(row);
                const keyStr = key.some((x) => x === null || x === undefined) ? "" : encodeRowKey(key as string[]);
                const title = displayCol ? formatCell(displayCol, row[displayCol.name], lang) : String(key.join(" | "));
                return (
                  <div key={keyStr + JSON.stringify(row)} draggable
                    onDragStart={() => setDrag({ row, from: v })}
                    className={`cursor-grab rounded-md border bg-card p-2.5 shadow-xs active:cursor-grabbing ${busy === keyStr ? "opacity-50" : ""}`}>
                    <p className="text-xs font-medium break-words">{title}</p>
                    <div className="mt-1.5 flex items-center justify-between">
                      <span className="text-[10px] font-mono text-muted-foreground">{v === "" ? "—" : String(row[boardCol.name])}</span>
                      <div className="flex gap-1">
                        <button onClick={() => onEdit(row)} className="text-muted-foreground hover:text-foreground text-[10px]">Edit</button>
                        {keyStr && <button onClick={() => onDelete(key as string[])} className="text-muted-foreground hover:text-destructive text-[10px]">Del</button>}
                      </div>
                    </div>
                  </div>
                );
              })}
              {st.loading && <p className="py-2 text-center text-[10px] text-muted-foreground">Loading…</p>}
              {!st.loading && st.rows.length >= COL_CAP && (
                <p className="py-2 text-center text-[10px] text-muted-foreground">Board truncated ({COL_CAP} cards)</p>
              )}
            </div>
            <button onClick={() => loadMore(v)} disabled={st.loading}
              className="border-t py-1.5 text-[10px] text-muted-foreground hover:bg-muted/40">
              {st.loading ? "Loading…" : "Load more"}
            </button>
          </div>
        );
      })}
    </div>
  );
}
