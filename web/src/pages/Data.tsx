import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate, Link, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Search,
  Plus,
  ExternalLink,
  RefreshCw,
  Edit,
  Trash2,
  Copy,
  AlertTriangle,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  ChevronLeft,
  ChevronRight,
  Layers,
  Database,
  Download,
  Upload,
  Settings2,
} from "lucide-react";
import { api, ApiError } from "../lib/api";
import { encodeRowKey } from "../lib/rowkey";
import { enumColorClass, formatCell } from "../lib/format";
import type { ColumnDef, FkOptionsRes, Me, Row, RowsRes, TableDefPayload, ViewConfig, ViewMode } from "../lib/types";
import { HelpPopover } from "../components/ColumnListEditor";
import { FilterBar, serializeFilters, type ActiveFilter } from "../components/FilterBar";
import { KanbanView } from "../components/views/KanbanView";
import { CalendarView } from "../components/views/CalendarView";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";

export default function Data() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const autoEditParam = searchParams.get("autoEdit");
  const searchParam = searchParams.get("search") || "";

  const qc = useQueryClient();
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/auth/me") });
  const [search, setSearch] = useState(searchParam);
  const [debounced, setDebounced] = useState(searchParam);
  const [sort, setSort] = useState("");
  const [dir, setDir] = useState<"ASC" | "DESC">("ASC");
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<ActiveFilter[]>([]);
  const [groupBy, setGroupBy] = useState("");
  const [drift, setDrift] = useState<{ missing: string[]; added: string[]; typeChanged: string[] } | null>(null);
  const [connErr, setConnErr] = useState("");
  const [form, setForm] = useState<{ mode: "new" | "edit"; row: Row; initialKey?: string[] | null } | null>(null);
  // bumped after any row mutation lands (save/delete/bulk/resync) so the
  // kanban and calendar views re-fetch their data
  const [dataVersion, setDataVersion] = useState(0);

  // view chosen by the user this session; null = follow the definition's
  // default view (re-derived whenever the definition changes)
  const [view, setView] = useState<ViewMode | null>(null);
  const vc: ViewConfig = def.data?.viewConfig ?? {};
  const viewOk: Record<ViewMode, boolean> = {
    grid: true,
    kanban: !!vc.kanbanBoardColumn,
    calendar: !!vc.calendarStartColumn,
  };
  const fallbackView: ViewMode = viewOk[def.data?.defaultView ?? "grid"] ? (def.data?.defaultView ?? "grid") : "grid";
  const effectiveView: ViewMode = view !== null && viewOk[view] ? view : fallbackView;

  // Sync search & table state when table id or URL search changes
  useEffect(() => {
    setSearch(searchParam);
    setDebounced(searchParam);
    setPage(1);
    setSort("");
    setDir("ASC");
    setFilters([]);
    setGroupBy("");
    setForm(null);
    setView(null);
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [id, searchParam]);

  // apply the definition's default sort once the def is loaded and no
  // explicit sort has been chosen
  useEffect(() => {
    if (!def.data) return;
    setSort((cur) => (cur === "" && def.data?.defaultSortCol ? def.data.defaultSortCol : cur));
    setDir((cur) => (cur === "ASC" && def.data?.defaultSortCol ? (def.data.defaultSortDir === "DESC" ? "DESC" : "ASC") : cur));
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [def.data?.id]);

  useEffect(() => {
    const t = setTimeout(() => {
      setDebounced(search);
      setPage(1);
    }, 300);
    return () => clearTimeout(t);
  }, [search]);

  const verify = useMutation({
    mutationFn: async () => {
      try {
        await api(`/tables/${id}/verify`);
        setDrift(null);
        setConnErr("");
      } catch (e) {
        if (e instanceof ApiError && e.code === "DRIFT") setDrift(e.detail as never);
        else if (e instanceof ApiError && e.code === "CONN") setConnErr(e.message + ": " + String(e.detail ?? ""));
        else throw e;
      }
    },
  });

  useEffect(() => {
    verify.mutate();
    /* eslint-disable-line */
  }, [id]);

  const rows = useQuery({
    queryKey: ["rows", id, debounced, sort, dir, page, filters],
    enabled: !!def.data,
    queryFn: () => {
      const p = new URLSearchParams();
      if (debounced) p.set("search", debounced);
      const fs = serializeFilters(filters);
      if (fs) p.set("filters", fs);
      if (sort) {
        p.set("sort", sort);
        p.set("dir", dir);
      }
      p.set("page", String(page));
      return api<RowsRes>(`/tables/${id}/rows?${p}`);
    },
  });

  // grouped grid fetch: pulls ALL matching rows in chunks (capped) so the
  // client can bucket them by the chosen column's value (grid view only).
  // keyed on dataVersion so save/delete/bulk/resync refresh the groups too.
  const groupedRows = useQuery({
    queryKey: ["rows-grouped", id, debounced, sort, dir, filters, groupBy, dataVersion],
    enabled: !!def.data && !!groupBy && effectiveView === "grid",
    queryFn: async () => {
      const all: Row[] = [];
      const CAP = 2000;
      for (let page = 1; ; page++) {
        const p = new URLSearchParams();
        if (debounced) p.set("search", debounced);
        const fs = serializeFilters(filters);
        if (fs) p.set("filters", fs);
        if (sort) {
          p.set("sort", sort);
          p.set("dir", dir);
        }
        p.set("page", String(page));
        p.set("limit", String(Math.min(200, CAP)));
        const res = await api<RowsRes>(`/tables/${id}/rows?${p}`);
        all.push(...res.rows);
        if (all.length >= res.total || res.rows.length === 0 || all.length >= CAP) break;
      }
      return { rows: all, truncated: all.length >= CAP };
    },
  });

  const cols = useMemo(() => (def.data?.columns ?? []).filter((c) => c.visible), [def.data]);
  // columns the grid can group by: plain stored values only (no computed,
  // m2m or fk — those have no single raw value to bucket on)
  const groupable = cols.filter((c) => !c.isComputed && c.fieldType !== "m2m" && c.fieldType !== "fk");
  const groupByLabel = cols.find((c) => c.name === groupBy)?.label ?? groupBy;
  const editable = cols.filter((c) => c.editable);
  const keyCols = def.data?.keyColumns ?? [];
  const perms = def.data?.permissions ?? { read: false, create: false, update: false, delete: false };
  // composite key value for a row, or null when any key part is NULL
  const rowKey = (row: Row): string[] | null => {
    const vals = keyCols.map((c) => row[c]);
    if (vals.some((v) => v === null || v === undefined)) return null;
    return vals as string[];
  };

  // pretty-print json column values once, when form state is created (keeps
  // the textarea a plain controlled input while editing)
  const prettifyFormRow = (row: Row): Row => {
    const out: Row = { ...row };
    for (const c of def.data?.columns ?? []) {
      if (c.fieldType === "json" && typeof out[c.name] === "string") {
        out[c.name] = prettyJSON(out[c.name] as string);
      }
    }
    return out;
  };

  const handleCopy = (row: Row) => {
    const copiedRow: Row = prettifyFormRow(row);
    for (const k of keyCols) {
      delete copiedRow[k];
    }
    setForm({ mode: "new", row: copiedRow });
  };

  // Auto-open edit modal if navigated here with autoEdit parameter once fresh rows for current id have loaded
  useEffect(() => {
    if (!autoEditParam || !rows.data?.rows || rows.isFetching || form) return;
    if (def.data?.id !== id) return;
    const target = rows.data.rows.find((r) => {
      return Object.values(r).some((v) => v !== null && v !== undefined && String(v) === autoEditParam);
    });
    if (target) {
      const k = rowKey(target);
      setForm({ mode: "edit", row: prettifyFormRow({ ...target }), initialKey: k });
      const next = new URLSearchParams(searchParams);
      next.delete("autoEdit");
      setSearchParams(next, { replace: true });
    }
  }, [autoEditParam, rows.data, rows.isFetching, def.data, id, form, searchParams, setSearchParams, keyCols]);


  const modalFields = useMemo(() => {
    const allCols = def.data?.columns ?? [];
    return allCols.filter((c) => c.editable || keyCols.includes(c.name) || c.fieldType === "m2m" || c.isComputed);
  }, [def.data?.columns, keyCols]);

  const save = useMutation({
    mutationFn: async () => {
      const payload: Row = {};
      if (form!.mode === "new") {
        // Send all fields filled in the insert modal (including PK if provided)
        for (const c of modalFields) {
          const v = form!.row[c.name];
          if (v !== undefined && v !== null && v !== "") {
            payload[c.name] = v;
          }
        }
        await api(`/tables/${id}/rows`, { method: "POST", body: JSON.stringify(payload) });
      } else {
        // In edit mode, strip PKs and non-editable fields from PUT payload
        // (m2m selections always ride along — the server strips and syncs them)
        const sendCols = [
          ...editable.filter((c) => !keyCols.includes(c.name)),
          ...(def.data?.columns ?? []).filter((c) => c.fieldType === "m2m"),
        ];
        for (const c of sendCols) {
          const v = form!.row[c.name];
          if (v !== undefined) payload[c.name] = v;
        }
        const key = form!.initialKey || rowKey(form!.row);
        if (!key) throw new Error("row has a null key value");
        await api(`/tables/${id}/rows/${encodeRowKey(key)}`, { method: "PUT", body: JSON.stringify(payload) });
      }
    },
    onSuccess: () => {
      setForm(null);
      rows.refetch();
      setDataVersion((v) => v + 1);
    },
  });

  const [delErr, setDelErr] = useState("");

  // CSV export follows the active search/sort/filters; all pages, not just current
  const [exporting, setExporting] = useState(false);
  const exportCSV = async () => {
    setExporting(true);
    try {
      const p = new URLSearchParams();
      if (debounced) p.set("search", debounced);
      if (sort) {
        p.set("sort", sort);
        p.set("dir", dir);
      }
      const fs = serializeFilters(filters);
      if (fs) p.set("filters", fs);
      const res = await fetch(`/api/tables/${id}/rows/export?${p}`, { credentials: "same-origin" });
      if (!res.ok) {
        let msg = `HTTP ${res.status}`;
        try {
          const e = await res.json();
          msg = e.message ? `${e.message}` : msg;
        } catch { /* not json */ }
        alert(`Export failed: ${msg}`);
        return;
      }
      const cd = res.headers.get("Content-Disposition") || "";
      const m = /filename="?([^";]+)"?/.exec(cd);
      const name = m?.[1] ?? `${def.data?.tableName ?? "table"}.csv`;
      const url = URL.createObjectURL(await res.blob());
      const a = document.createElement("a");
      a.href = url;
      a.download = name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } finally {
      setExporting(false);
    }
  };
  const del = useMutation({
    mutationFn: (key: string[]) => api(`/tables/${id}/rows/${encodeRowKey(key)}`, { method: "DELETE" }),
    onSuccess: () => {
      setDelErr("");
      rows.refetch();
      setDataVersion((v) => v + 1);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "CONFLICT") {
        const d = Array.isArray(e.detail)
          ? (e.detail as { table: string; column: string; count: number }[])
              .map((x) => `${x.table}.${x.column} (${x.count} rows)`).join(", ")
          : String(e.detail ?? "");
        setDelErr(`Row direferensikan oleh: ${d || "table lain"}`);
      } else {
        setDelErr(e instanceof Error ? e.message : "delete failed");
      }
    },
  });

  // multi-select bulk delete (chunked requests, per-row results)
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [bulkMsg, setBulkMsg] = useState("");
  const [bulkBusy, setBulkBusy] = useState(false);
  const toggleSel = (key: string) => {
    setSel((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const pageKeys = (rows.data?.rows ?? [])
    .map((row) => ({ key: rowKey(row) ? encodeRowKey(rowKey(row) as string[]) : "", row }))
    .filter((x) => x.key !== "");
  const allPageSelected = pageKeys.length > 0 && pageKeys.every((x) => sel.has(x.key));
  const runBulkDelete = async () => {
    if (!confirm(`Delete ${sel.size} selected rows? This cannot be undone.`)) return;
    setBulkBusy(true);
    setBulkMsg("");
    try {
      const keys = [...sel];
      let deleted = 0;
      const failed: { key: string; code: string; message: string }[] = [];
      for (let i = 0; i < keys.length; i += 500) {
        const res = await api<{ deleted: number; failures: { key: string; code: string; message: string }[] }>(
          `/tables/${id}/rows/bulk-delete`,
          { method: "POST", body: JSON.stringify({ keys: keys.slice(i, i + 500) }) }
        );
        deleted += res.deleted;
        failed.push(...res.failures);
      }
      setBulkMsg(
        failed.length
          ? `${deleted} deleted, ${failed.length} failed: ` +
              failed.slice(0, 5).map((f) => `[${f.code}] ${f.message}`).join("; ") +
              (failed.length > 5 ? ` … (+${failed.length - 5} more)` : "")
          : `${deleted} rows deleted.`
      );
      setSel(new Set());
      rows.refetch();
      setDataVersion((v) => v + 1);
    } catch (e) {
      setBulkMsg(e instanceof Error ? e.message : "bulk delete failed");
    } finally {
      setBulkBusy(false);
    }
  };

  const resync = useMutation({
    mutationFn: () => api(`/tables/${id}/resync`, { method: "POST" }),
    onSuccess: () => {
      setDrift(null);
      qc.invalidateQueries({ queryKey: ["def", id] });
      rows.refetch();
      setDataVersion((v) => v + 1);
    },
  });

  if (def.isLoading) {
    return (
      <div className="flex h-64 items-center justify-center text-xs text-muted-foreground">
        Loading table dynamic schema...
      </div>
    );
  }

  if (!def.data) {
    return (
      <div className="flex flex-col items-center justify-center h-64 space-y-3">
        <AlertTriangle className="h-8 w-8 text-destructive" />
        <p className="text-sm font-semibold text-destructive">Table definition not found</p>
        <Link to="/">
          <Button variant="outline" size="sm">
            Back to Tables
          </Button>
        </Link>
      </div>
    );
  }

  const r = rows.data;
  const d = def.data;
  const pages = r ? Math.max(1, Math.ceil(r.total / r.pageSize)) : 1;
  // total grid columns: optional bulk-select checkbox + data cols + actions
  const colsLen = cols.length + (perms.delete ? 2 : 1);

  const rels = rows.data?.rels;
  const m2mRels = rows.data?.m2mRels;
  const fkDisplay = (c: ColumnDef, row: Row): string | null => {
    if (c.fieldType === "m2m") {
      if (!c.m2mRefColumn) return "";
      const list = m2mRels?.[c.name]?.[String(row[c.m2mRefColumn])];
      if (!list || list.length === 0) return "";
      return list
        .map((tr) =>
          (c.m2mDisplayColumns ?? [])
            .map((f) => (tr[f] === null || tr[f] === undefined ? null : String(tr[f])))
            .filter((p): p is string => p !== null)
            .join(" — ")
        )
        .join(", ");
    }
    if (c.fieldType !== "fk" || !c.fkDisplayColumns) return null;
    const rel = rels?.[c.name]?.[String(row[c.name])];
    if (!rel) return null;
    const parts = c.fkDisplayColumns
      .map((f) => (rel[f] === null || rel[f] === undefined ? null : String(rel[f])))
      .filter((p): p is string => p !== null);
    return parts.length ? parts.join(" — ") : null;
  };

  // single grid row, shared by the flat paginated body and the grouped body
  // (the react key is applied at the call site)
  const RowRow = ({ row }: { row: Row }) => {
    const rowKeyStr = rowKey(row) ? encodeRowKey(rowKey(row) as string[]) : "";
    return (
      <TableRow className="hover:bg-muted/20">
        {perms.delete && (
          <TableCell className="w-10">
            {rowKeyStr && (
              <Checkbox
                aria-label="Select row"
                checked={sel.has(rowKeyStr)}
                onChange={() => toggleSel(rowKeyStr)}
              />
            )}
          </TableCell>
        )}
        {cols.map((c) => {
          const disp = fkDisplay(c, row);
          return (
            <TableCell
              key={c.name}
              className={`text-xs font-mono max-w-xs ${c.fieldType === "json" ? "align-top" : "truncate"}`}
            >
              {disp !== null ? (
                <span className="font-sans">{disp}</span>
              ) : c.fieldType === "enum" ? (
                <Badge variant="outline" className={`text-[10px] ${enumColorClass(c, String(row[c.name]))}`}>
                  {row[c.name] === null || row[c.name] === undefined ? <span className="italic text-muted-foreground/50">—</span> : String(row[c.name])}
                </Badge>
              ) : c.fieldType === "number" || c.fieldType === "datetime" ? (
                <span className="font-sans">
                  {formatCell(c, row[c.name], me.data?.language ?? "en") || <span className="italic text-muted-foreground/50">—</span>}
                </span>
              ) : (
                renderValue(row[c.name], c.fieldType === "fk" ? "text" : c.fieldType)
              )}
            </TableCell>
          );
        })}
        <TableCell className="text-right">
          <div className="flex items-center justify-end gap-1">
            {perms.create && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-blue-600"
                onClick={() => handleCopy(row)}
                title="Copy / Duplicate record"
              >
                <Copy className="h-3.5 w-3.5" />
              </Button>
            )}
            {rowKey(row) && perms.update && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-foreground"
                onClick={() => setForm({ mode: "edit", row: prettifyFormRow({ ...row }), initialKey: rowKey(row) })}
                title="Edit row"
              >
                <Edit className="h-3.5 w-3.5" />
              </Button>
            )}
            {rowKey(row) && perms.delete && (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-destructive"
                onClick={() => {
                  const key = rowKey(row);
                  if (key && confirm("Delete this record permanently?")) del.mutate(key);
                }}
                title="Delete row"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        </TableCell>
      </TableRow>
    );
  };

  return (
    <div className="space-y-6">
      {/* Top Header & Search Bar */}
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between border-b pb-4">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
            <Layers className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-xl font-bold tracking-tight">{def.data.label}</h2>
              <Badge variant="outline" className="font-mono text-[10px] bg-muted/60">
                {def.data.schemaName}.{def.data.tableName}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground mt-0.5">
              Key: <span className="font-mono text-foreground font-semibold">{keyCols.join(" + ")}</span> &bull; {r?.total ?? 0} total records
            </p>
          </div>
        </div>

        <div>
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <FilterBar key={id} cols={cols} filters={filters} onChange={(fs) => { setFilters(fs); setPage(1); }} />
            {effectiveView === "grid" && groupable.length > 0 && (
              <Select value={groupBy || "none"} onValueChange={(v) => { setGroupBy(v === "none" ? "" : v); setPage(1); }}>
                <SelectTrigger className="h-8 w-36 text-xs"><SelectValue placeholder="Group by…" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="none" className="text-xs">No grouping</SelectItem>
                  {groupable.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs font-mono">{c.label}</SelectItem>)}
                </SelectContent>
              </Select>
            )}
          </div>
          <div className="flex items-center gap-2">
          {cols.some((c) => c.searchable) && (
            <div className="flex items-center gap-1">
              <div className="relative">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search rows..."
                  className="h-9 w-48 pl-8 text-xs md:w-64"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </div>
              <HelpPopover title="Search Guide" placement="bottom">
                <p>Filters rows across all searchable columns using parameterized SQL <code>LIKE</code> wildcard matching.</p>
              </HelpPopover>
            </div>
          )}
          <div className="flex items-center gap-0.5 rounded-lg border border-border/60 bg-muted/40 p-0.5">
            {(["grid", "kanban", "calendar"] as const).filter((v) => viewOk[v]).map((v) => (
              <button key={v} onClick={() => setView(v)}
                className={cn("rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                  effectiveView === v ? "bg-card shadow-sm text-foreground" : "text-muted-foreground hover:text-foreground")}>
                {v === "grid" ? "Grid" : v === "kanban" ? "Kanban" : "Calendar"}
              </button>
            ))}
          </div>
          {me.data?.platformManage && (
            <Button
              variant="outline"
              size="sm"
              className="h-9 gap-1 text-xs"
              onClick={() => navigate(`/tables/${id}/edit`)}
              title="Open table definition"
            >
              <Settings2 className="h-3.5 w-3.5" /> Definition
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            className="h-9 gap-1 text-xs"
            onClick={() => rows.refetch()}
            title="Refresh rows"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${rows.isFetching ? "animate-spin" : ""}`} />
          </Button>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-9 gap-1 text-xs"
              onClick={() => exportCSV()}
              disabled={exporting}
              title="Export the current result (search & sort applied) as CSV"
            >
              <Download className="h-3.5 w-3.5" />
              {exporting ? "Exporting..." : "Export CSV"}
            </Button>
            <HelpPopover title="Export CSV Guide" placement="bottom">
              <p>Downloads all matching records (up to 100,000 rows) with active search and sort filters applied as a UTF-8 BOM CSV file.</p>
              <p className="pt-1 text-[10px]">💡 FK and Many-to-Many relations are resolved into human-readable display titles.</p>
            </HelpPopover>
          </div>
          {perms.create && (
            <>
              <div className="flex items-center gap-1">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-9 gap-1 text-xs"
                  onClick={() => navigate(`/data/${id}/import`)}
                  title="Import rows from a CSV file"
                >
                  <Upload className="h-3.5 w-3.5" /> Import CSV
                </Button>
                <HelpPopover title="Import CSV Guide" placement="bottom">
                  <p>Bulk upload records from a CSV file (up to 5MB / 10,000 rows).</p>
                  <p className="pt-1 text-[10px]">💡 Includes delimiter sniffing (comma/semicolon/tab), header mapping, and per-row validation preview.</p>
                </HelpPopover>
              </div>
              <Button
                onClick={() => setForm({ mode: "new", row: {} })}
                className="h-9 bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1 text-xs"
              >
                <Plus className="h-4 w-4" /> New Row
              </Button>
            </>
          )}
          </div>
        </div>
      </div>

      {/* Connection Error Banner */}
      {connErr && (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/10 p-3.5 text-xs text-destructive">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>{connErr}</span>
        </div>
      )}

      {/* Schema Drift Warning Banner */}
      {drift && (
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-xs text-amber-700 dark:text-amber-300">
          <div className="flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
            <div>
              <p className="font-semibold">Schema Drift Detected!</p>
              <p className="text-[11px] text-amber-600/90 dark:text-amber-300/80 mt-0.5">
                The database schema has changed since this table was mapped.
                {["missing", "added", "typeChanged"].map((k) => {
                  const v = (drift as never as Record<string, string[]>)[k] ?? [];
                  return v.length ? (
                    <span key={k} className="ml-1 font-mono">
                      {k}: [{v.join(", ")}]
                    </span>
                  ) : null;
                })}
              </p>
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-8 border-amber-500/40 text-xs text-amber-700 dark:text-amber-300 hover:bg-amber-500/20 shrink-0"
            onClick={() => resync.mutate()}
            disabled={resync.isPending}
          >
            <RefreshCw className={`h-3.5 w-3.5 mr-1 ${resync.isPending ? "animate-spin" : ""}`} />
            Re-sync Definition
          </Button>
        </div>
      )}

      {delErr && (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/10 p-3.5 text-xs text-destructive">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span className="flex-1">{delErr}</span>
          <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setDelErr("")}>Tutup</Button>
        </div>
      )}

      {/* Bulk selection toolbar */}
      {(sel.size > 0 || bulkMsg) && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-blue-500/30 bg-blue-500/5 p-3 text-xs">
          <div className="flex items-center gap-2">
            <Badge variant="secondary" className="bg-blue-500/10 text-blue-600 border-blue-500/20">
              {sel.size} selected
            </Badge>
            {bulkMsg && <span className="text-muted-foreground">{bulkMsg}</span>}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => { setSel(new Set()); setBulkMsg(""); }}>
              Clear
            </Button>
            <Button
              size="sm"
              className="h-7 gap-1 bg-red-600 text-white hover:bg-red-700 text-xs"
              disabled={sel.size === 0 || bulkBusy}
              onClick={() => runBulkDelete()}
            >
              <Trash2 className="h-3.5 w-3.5" /> {bulkBusy ? "Deleting..." : `Delete ${sel.size} rows`}
            </Button>
          </div>
        </div>
      )}

      {effectiveView === "kanban" && vc.kanbanBoardColumn && (() => {
        const boardCol = d.columns.find((c) => c.name === vc.kanbanBoardColumn)!;
        const displayCol = vc.kanbanDisplayColumn ? d.columns.find((c) => c.name === vc.kanbanDisplayColumn) : undefined;
        return (
          <KanbanView
            def={d} boardCol={boardCol} displayCol={displayCol} dataVersion={dataVersion}
            search={debounced} filters={serializeFilters(filters)} pageSize={d.pageSize}
            lang={me.data?.language ?? "en"}
            onEdit={(row) => setForm({ mode: "edit", row: prettifyFormRow({ ...row }), initialKey: rowKey(row) })}
            onDelete={(key) => { if (confirm("Delete this record permanently?")) del.mutate(key); }}
            onCreate={() => setForm({ mode: "new", row: {} })}
          />
        );
      })()}

      {effectiveView === "calendar" && vc.calendarStartColumn && (() => {
        const startCol = d.columns.find((c) => c.name === vc.calendarStartColumn)!;
        const endCol = vc.calendarEndColumn ? d.columns.find((c) => c.name === vc.calendarEndColumn) : undefined;
        return (
          <CalendarView
            def={d} startCol={startCol} endCol={endCol} dataVersion={dataVersion}
            filters={filters} search={debounced} pageSize={d.pageSize}
            lang={me.data?.language ?? "en"}
            onEdit={(row) => setForm({ mode: "edit", row: prettifyFormRow({ ...row }), initialKey: rowKey(row) })}
            onDayCreate={(date) => setForm({ mode: "new", row: { [startCol.name]: date } })}
          />
        );
      })()}

      {/* Main Data Table (grid view) */}
      {effectiveView === "grid" && (
      <Card className="border-border/60 shadow-sm overflow-hidden">
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/50 hover:bg-muted/50">
                  {perms.delete && (
                    <TableHead className="w-10">
                      <Checkbox
                        aria-label="Select all rows on this page"
                        checked={allPageSelected}
                        onChange={(e) => {
                          const on = e.target.checked;
                          setSel((prev) => {
                            const next = new Set(prev);
                            for (const pk of pageKeys) {
                              if (on) next.add(pk.key);
                              else next.delete(pk.key);
                            }
                            return next;
                          });
                        }}
                      />
                    </TableHead>
                  )}
                  {cols.map((c) => (
                    <TableHead
                      key={c.name}
                      className={c.sortable ? "cursor-pointer select-none hover:text-foreground transition-colors" : ""}
                      onClick={() => {
                        if (!c.sortable) return;
                        if (sort === c.name) {
                          setDir(dir === "ASC" ? "DESC" : "ASC");
                        } else {
                          setSort(c.name);
                          setDir("ASC");
                        }
                        setPage(1);
                      }}
                    >
                      <div className="flex items-center gap-1">
                        <span>{c.label}</span>
                        {c.isComputed && <Badge variant="outline" className="text-[9px] font-mono bg-emerald-500/10 text-emerald-600 border-emerald-500/20 ml-1">fx</Badge>}
                        {c.sortable && (
                          <span className="text-muted-foreground">
                            {sort === c.name ? (
                              dir === "ASC" ? (
                                <ArrowUp className="h-3 w-3 text-blue-500" />
                              ) : (
                                <ArrowDown className="h-3 w-3 text-blue-500" />
                              )
                            ) : (
                              <ArrowUpDown className="h-3 w-3 opacity-30" />
                            )}
                          </span>
                        )}
                      </div>
                    </TableHead>
                  ))}
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {groupBy ? (
                  !groupedRows.data ? (
                    <TableRow>
                      <TableCell colSpan={colsLen} className="h-24 text-center text-xs text-muted-foreground">
                        Fetching records...
                      </TableCell>
                    </TableRow>
                  ) : groupedRows.data.rows.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={colsLen} className="h-32 text-center">
                        <div className="flex flex-col items-center justify-center space-y-1">
                          <Database className="h-7 w-7 text-muted-foreground/30" />
                          <p className="text-xs font-medium text-muted-foreground">No records found</p>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : (() => {
                    const groups = new Map<string, Row[]>();
                    for (const row of groupedRows.data.rows) {
                      const v = row[groupBy] === null || row[groupBy] === undefined ? "" : String(row[groupBy]);
                      if (!groups.has(v)) groups.set(v, []);
                      groups.get(v)!.push(row);
                    }
                    const sections: React.ReactNode[] = [];
                    let gi = 0;
                    for (const [gv, rowsInGroup] of groups) {
                      sections.push(
                        <TableRow key={`g${gi++}`} className="bg-muted/60">
                          <TableCell colSpan={colsLen} className="px-4 py-1.5 text-xs font-semibold">
                            {gv || <span className="italic text-muted-foreground">—</span>}
                            <span className="ml-2 text-[10px] font-normal text-muted-foreground">({rowsInGroup.length})</span>
                          </TableCell>
                        </TableRow>
                      );
                      sections.push(...rowsInGroup.map((row, i) => <RowRow key={`r${gi}-${i}`} row={row} />));
                    }
                    return sections;
                  })()
                ) : rows.isLoading ? (
                  <TableRow>
                    <TableCell colSpan={colsLen} className="h-24 text-center text-xs text-muted-foreground">
                      Fetching records...
                    </TableCell>
                  </TableRow>
                ) : (r?.rows ?? []).length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={colsLen} className="h-32 text-center">
                      <div className="flex flex-col items-center justify-center space-y-1">
                        <Database className="h-7 w-7 text-muted-foreground/30" />
                        <p className="text-xs font-medium text-muted-foreground">No records found</p>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : (
                  (r?.rows ?? []).map((row, i) => <RowRow key={i} row={row} />)
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>

        {/* Footer: grouped banner or pagination */}
        {groupBy ? (
          <div className="flex items-center justify-between border-t bg-muted/20 px-4 py-3 text-xs text-muted-foreground">
            <span>
              Grouped {groupedRows.data?.rows.length ?? 0} rows by <strong className="text-foreground">{groupByLabel}</strong>
              {groupedRows.data?.truncated && <span className="ml-2 text-amber-600">— grouping truncated at 2,000 rows</span>}
            </span>
            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setGroupBy("")}>Clear grouping</Button>
          </div>
        ) : (
        <div className="flex items-center justify-between border-t bg-muted/20 px-4 py-3 text-xs text-muted-foreground">
          <span>
            Page <strong className="text-foreground">{page}</strong> of <strong className="text-foreground">{pages}</strong> &bull; Total {r?.total ?? 0} rows
          </span>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1 text-xs"
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              <ChevronLeft className="h-3.5 w-3.5" /> Prev
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1 text-xs"
              disabled={page >= pages}
              onClick={() => setPage(page + 1)}
            >
              Next <ChevronRight className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        )}
      </Card>
      )}

      {/* Row Editor Modal */}
      <Dialog open={!!form} onOpenChange={(o) => !o && setForm(null)}>
        <DialogContent className="max-w-3xl w-[90vw] max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <div className="flex items-center gap-2">
              <DialogTitle>{form?.mode === "new" ? "Insert New Record" : "Edit Record"}</DialogTitle>
              {form && (
                <HelpPopover placement="bottom-start" title={form.mode === "new" ? "Insert Record Guide" : "Edit Record Guide"}>
                  {form.mode === "new" ? (
                    <div className="space-y-1">
                      <p>
                        Provide values for new record fields.
                      </p>
                      <p className="pt-1 text-[10px]">
                        💡 <strong>Primary Key Policy:</strong> PK fields accept manual values or can be left blank if your database auto-generates IDs via sequence / SERIAL defaults.
                      </p>
                    </div>
                  ) : (
                    <div className="space-y-1">
                      <p>
                        Modify attribute values for existing record <strong>{keyCols.join(" + ")}</strong>.
                      </p>
                      <p className="pt-1 text-[10px]">
                        💡 <strong>Primary Key Policy:</strong> Primary Key fields are displayed as <strong>Read-Only</strong> on Edit Mode to protect record identity.
                      </p>
                    </div>
                  )}
                </HelpPopover>
              )}
            </div>
            <DialogDescription className="text-xs">
              {form?.mode === "new" ? "Provide values for editable table fields" : `Editing record ${keyCols.join(" + ")}`}
            </DialogDescription>
          </DialogHeader>

          {form && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 py-2">
              {modalFields.map((c) => {
                const isPkLockedInEdit = form.mode === "edit" && keyCols.includes(c.name);
                return c.isComputed ? (
                  <div key={c.name} className="space-y-1">
                    <Label className="text-xs font-medium">{c.label} <Badge variant="outline" className="ml-1 text-[9px] font-mono text-emerald-600 border-emerald-500/30 bg-emerald-500/10">fx</Badge></Label>
                    <div className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono">
                      {formatCell(c, form!.row[c.name], me.data?.language ?? "en") || <span className="italic text-muted-foreground/50">—</span>}
                    </div>
                  </div>
                ) : c.fieldType === "fk" ? (
                  <FkField
                    key={c.name}
                    col={c}
                    tableId={id}
                    row={form.row}
                    rels={rows.data?.rels}
                    onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })}
                  />
                ) : c.fieldType === "m2m" ? (
                  <div key={c.name} className="md:col-span-2">
                    <M2MField
                      col={c}
                      tableId={id}
                      mode={form.mode}
                      rowKey={form.initialKey || rowKey(form.row)}
                      value={(form.row[c.name] as unknown[]) ?? []}
                      onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })}
                    />
                  </div>
                ) : (
                  <FieldInput
                    key={c.name}
                    col={c}
                    row={form.row}
                    disabled={isPkLockedInEdit}
                    onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })}
                  />
                );
              })}
            </div>
          )}

          {save.isError && (
            <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive space-y-1">
              <p className="font-semibold">
                {(save.error as Error).message}: {String((save.error as ApiError).detail ?? "")}
              </p>
              {String((save.error as ApiError).detail ?? "").includes("violates not-null constraint") && (
                <p className="text-[11px] opacity-90 border-t pt-1 border-destructive/20 font-sans">
                  💡 <strong>Solution Tip:</strong> This column is defined as <code>NOT NULL</code> in the database without an auto-increment sequence default. Go to <strong>Table Definitions</strong> (Step 3) and enable the <strong>Edit</strong> switch for this column so it appears on the form, or add a <code>DEFAULT nextval(...)</code> / <code>SERIAL</code> sequence in your database.
                </p>
              )}
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0">
            <Button variant="outline" onClick={() => setForm(null)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                const missing = modalFields.filter(
                  (c) => c.required && !keyCols.includes(c.name) && (form!.row[c.name] === undefined || form!.row[c.name] === null || form!.row[c.name] === "")
                );
                if (missing.length) {
                  return alert(`Required fields missing: ${missing.map((c) => c.label).join(", ")}`);
                }
                const badJson = modalFields.filter(
                  (c) => c.fieldType === "json" && typeof form!.row[c.name] === "string" &&
                    (() => { try { JSON.parse(form!.row[c.name] as string); return false; } catch { return true; } })()
                );
                if (badJson.length) {
                  return alert(`Invalid JSON: ${badJson.map((c) => c.label).join(", ")}`);
                }
                save.mutate();
              }}
              disabled={save.isPending}
              className="bg-blue-600 text-white hover:bg-blue-700"
            >
              {save.isPending ? "Saving..." : "Save Record"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function renderValue(v: unknown, type: ColumnDef["fieldType"]): React.ReactNode {
  if (v === null || v === undefined) {
    return <span className="text-muted-foreground/50 font-sans italic">—</span>;
  }
  if (type === "boolean" || typeof v === "boolean") {
    return (
      <Badge variant={v ? "secondary" : "outline"} className={`text-[10px] ${v ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20" : "text-muted-foreground"}`}>
        {v ? "true" : "false"}
      </Badge>
    );
  }
  if (type === "enum") {
    return <Badge variant="outline" className="text-[10px] bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20">{String(v)}</Badge>;
  }
  if (type === "uuid") {
    return <span className="font-mono text-xs">{String(v)}</span>;
  }
  if (type === "json") {
    const pretty = prettyJSON(String(v));
    return (
      <pre
        title={pretty}
        className="whitespace-pre-wrap font-mono text-[11px] leading-snug text-foreground/90"
        style={{ display: "-webkit-box", WebkitLineClamp: 3, WebkitBoxOrient: "vertical", overflow: "hidden" }}
      >
        {pretty}
      </pre>
    );
  }
  return String(v);
}

// prettyJSON reformats a JSON string for readable grid/form display; invalid
// JSON falls back to the raw string (server still validates on submit).
export function prettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function FieldInput({
  col, row, disabled = false, onChange,
}: {
  col: ColumnDef; row: Row; disabled?: boolean; onChange: (v: unknown) => void;
}) {
  const raw = row[col.name];
  const val = raw === null || raw === undefined ? "" : String(raw);

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <Label className="text-xs font-medium">
          {col.label} {col.required && <span className="text-destructive">*</span>}
          {disabled && <span className="text-[10px] text-muted-foreground font-mono ml-1.5">(Read Only)</span>}
        </Label>
        <span className="text-[10px] text-muted-foreground font-mono">{col.fieldType}</span>
      </div>

      {col.fieldType === "boolean" ? (
        <div className="flex items-center gap-2 pt-1">
          <Switch checked={raw === true} disabled={disabled} onCheckedChange={onChange} />
          <span className="text-xs font-mono">{raw === true ? "TRUE" : "FALSE"}</span>
        </div>
      ) : col.fieldType === "enum" ? (
        <Select value={val || undefined} disabled={disabled} onValueChange={onChange}>
          <SelectTrigger className="h-9 text-xs">
            <SelectValue placeholder="Select option..." />
          </SelectTrigger>
          <SelectContent>
            {(col.enumOptions ?? []).map((o) => (
              <SelectItem key={o} value={o} className="text-xs">
                {o}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : col.fieldType === "datetime" ? (
        <Input
          type="datetime-local"
          disabled={disabled}
          className="h-9 text-xs"
          value={val.slice(0, 16)}
          onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)}
        />
      ) : col.fieldType === "number" ? (
        <Input
          type="number"
          disabled={disabled}
          className="h-9 text-xs"
          value={val}
          onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
        />
      ) : col.fieldType === "json" ? (
        <Textarea
          disabled={disabled}
          className="min-h-[100px] font-mono text-xs"
          value={val}
          onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)}
        />
      ) : col.fieldType === "uuid" ? (
        <Input disabled={disabled} className="h-9 font-mono text-xs" value={val} placeholder="00000000-0000-0000-0000-000000000000" onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)} />
      ) : (
        <Input disabled={disabled} className="h-9 text-xs" value={val} onChange={(e) => onChange(e.target.value)} />
      )}
    </div>
  );
}

function FkField({
  col, tableId, row, rels, onChange,
}: {
  col: ColumnDef; tableId?: string; row: Row;
  rels?: Record<string, Record<string, Row>>; onChange: (v: unknown) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [page, setPage] = useState(1);
  const [selectedRel, setSelectedRel] = useState<Row | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const t = setTimeout(() => { setDebounced(search); setPage(1); }, 300);
    return () => clearTimeout(t);
  }, [search]);

  const opts = useQuery({
    queryKey: ["fkopts", tableId, col.name, debounced, page],
    enabled: open,
    queryFn: () =>
      api<FkOptionsRes>(
        `/tables/${tableId}/fkoptions/${col.name}?` +
          new URLSearchParams({ ...(debounced ? { search: debounced } : {}), page: String(page) })
      ),
  });

  const rel = rels?.[col.name]?.[String(row[col.name])];
  const activeRel = selectedRel || rel;
  const displayCols = col.fkDisplayColumns && col.fkDisplayColumns.length > 0
    ? col.fkDisplayColumns
    : col.fkRefColumn ? [col.fkRefColumn] : [];

  const pages = opts.data ? Math.max(1, Math.ceil(opts.data.total / opts.data.pageSize)) : 1;

  const handleEditRelated = () => {
    if (!col.fkTableDefId || col.fkTableDefId === "self") return;
    const refVal = row[col.name] !== null && row[col.name] !== undefined ? String(row[col.name]) : "";
    if (refVal) {
      navigate(`/data/${col.fkTableDefId}?search=${encodeURIComponent(refVal)}&autoEdit=${encodeURIComponent(refVal)}`);
    } else {
      navigate(`/data/${col.fkTableDefId}`);
    }
  };

  return (
    <div className="space-y-2 md:col-span-2 rounded-lg border border-border/80 bg-muted/20 p-3.5">
      {/* FK Header & Action Buttons */}
      <div className="flex items-center justify-between pb-1 border-b border-border/40">
        <div className="flex items-center gap-2">
          <Label className="text-xs font-semibold text-foreground">
            {col.label} {col.required && <span className="text-destructive">*</span>}
          </Label>
          <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20">
            FK ({col.name})
          </Badge>
        </div>
        <div className="flex items-center gap-1.5">
          {row[col.name] !== null && row[col.name] !== undefined && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 text-[11px] gap-1 border-blue-500/30 text-blue-600 dark:text-blue-400 hover:bg-blue-500/10"
              onClick={handleEditRelated}
              title="Edit record relasi di halaman tabelnya"
            >
              <ExternalLink className="h-3.5 w-3.5" /> Edit di table terkait
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 text-[11px] gap-1"
            onClick={() => setOpen(true)}
          >
            <Search className="h-3.5 w-3.5" /> Pilih…
          </Button>
          {row[col.name] !== null && row[col.name] !== undefined && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 text-[11px] text-muted-foreground hover:text-destructive"
              onClick={() => { setSelectedRel(null); onChange(null); }}
            >
              Clear
            </Button>
          )}
        </div>
      </div>

      {/* Individual Read-Only Display Fields */}
      {row[col.name] === null || row[col.name] === undefined ? (
        <div className="text-xs text-muted-foreground italic py-2 px-1">
          Belum ada relasi dipilih (Kosong)
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5 pt-1">
          {displayCols.map((fieldName) => {
            const val = activeRel?.[fieldName];
            const displayVal = val === null || val === undefined ? "" : String(val);
            return (
              <div key={fieldName} className="space-y-1">
                <Label className="text-[11px] text-muted-foreground font-medium">
                  {fieldName}
                </Label>
                <Input
                  readOnly
                  disabled
                  value={displayVal}
                  placeholder="— not set —"
                  className="h-8 text-xs font-mono bg-muted/50 cursor-not-allowed opacity-90 text-foreground"
                />
              </div>
            );
          })}
        </div>
      )}

      {/* FK Picker Modal */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Pilih {col.label}</DialogTitle>
            <DialogDescription className="text-xs">Cari lalu klik baris untuk menghubungkan</DialogDescription>
          </DialogHeader>
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input className="h-9 pl-8 text-xs" placeholder="Search..." value={search} onChange={(e) => setSearch(e.target.value)} />
          </div>
          <div className="max-h-64 overflow-y-auto rounded-md border">
            {(opts.data?.rows ?? []).length === 0 ? (
              <p className="p-4 text-center text-xs text-muted-foreground">Tidak ada data ditemukan</p>
            ) : (
              (opts.data?.rows ?? []).map((r, i) => (
                <button
                  key={i}
                  type="button"
                  className="flex w-full items-center justify-between border-b px-3 py-2 text-left text-xs hover:bg-muted/50 transition-colors"
                  onClick={() => {
                    setSelectedRel(r);
                    onChange(r[col.fkRefColumn ?? ""]);
                    setOpen(false);
                  }}
                >
                  <span className="font-mono">
                    {(col.fkDisplayColumns ?? []).map((f) => String(r[f] ?? "—")).join(" — ")}
                  </span>
                  <Badge variant="outline" className="text-[10px] font-mono">
                    {String(r[col.fkRefColumn ?? ""])}
                  </Badge>
                </button>
              ))
            )}
          </div>
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <Button
              type="button" variant="outline" size="sm" className="h-8 gap-1 text-xs"
              disabled={!col.fkTableDefId || col.fkTableDefId === "self"}
              onClick={() => col.fkTableDefId && navigate(`/data/${col.fkTableDefId}`)}
            >
              <Plus className="h-3.5 w-3.5" /> Tambah baru
            </Button>
            <div className="flex items-center gap-1">
              <Button type="button" variant="outline" size="sm" className="h-8 text-xs" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="h-3.5 w-3.5" />
              </Button>
              <span>Page {opts.data?.page ?? page} of {pages}</span>
              <Button type="button" variant="outline" size="sm" className="h-8 text-xs" disabled={page >= pages} onClick={() => setPage(page + 1)}>
                <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// M2MField manages a many-to-many selection: chips of picked target records
// plus a multi-select picker dialog reusing the fk picker's search pattern.
function M2MField({
  col, tableId, mode, rowKey, value, onChange,
}: {
  col: ColumnDef; tableId?: string; mode: "new" | "edit";
  rowKey: string[] | null | undefined;
  value: unknown[];
  onChange: (v: unknown[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [page, setPage] = useState(1);
  const targetRef = col.m2mTargetRef ?? "id";

  useEffect(() => {
    const t = setTimeout(() => { setDebounced(search); setPage(1); }, 300);
    return () => clearTimeout(t);
  }, [search]);

  const opts = useQuery({
    queryKey: ["m2mopts", tableId, col.name, debounced, page],
    enabled: open,
    queryFn: () =>
      api<FkOptionsRes>(
        `/tables/${tableId}/m2moptions/${col.name}?` +
          new URLSearchParams({ ...(debounced ? { search: debounced } : {}), page: String(page) })
      ),
  });

  // current selection display rows (edit mode only — keyed by the row key)
  const encodedKey = mode === "edit" && rowKey ? encodeRowKey(rowKey) : "";
  const links = useQuery({
    queryKey: ["m2mlinks", tableId, col.name, encodedKey],
    enabled: mode === "edit" && !!encodedKey,
    queryFn: () =>
      api<{ values: unknown[]; rows: Row[] }>(
        `/tables/${tableId}/rows/${encodedKey}/m2m/${col.name}`
      ),
  });

  // initialize the form value from links once loaded
  useEffect(() => {
    if (links.data && mode === "edit") {
      const cur = JSON.stringify(value ?? []);
      const loaded = JSON.stringify(links.data.values ?? []);
      if (cur !== loaded) {
        onChange(links.data.values ?? []);
      }
    }
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [links.data]);

  const selectedKeys = new Set((value ?? []).map((v) => String(v)));
  const labelOf = (v: unknown): string => {
    const row = links.data?.rows?.find(
      (r) => String(r[targetRef]) === String(v)
    );
    if (row) {
      const parts = (col.m2mDisplayColumns ?? [])
        .map((f) => (row[f] === null || row[f] === undefined ? null : String(row[f])))
        .filter((p): p is string => p !== null);
      if (parts.length) return parts.join(" — ");
    }
    return String(v);
  };

  const toggle = (v: unknown) => {
    const k = String(v);
    const next = (value ?? []).filter((x) => String(x) !== k);
    if (!selectedKeys.has(k)) next.push(v);
    onChange(next);
  };

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <Label className="text-xs font-medium">
          {col.label} <Badge variant="outline" className="ml-1 text-[9px] font-mono">m2m</Badge>
        </Label>
        <span className="text-[10px] text-muted-foreground font-mono">{value?.length ?? 0} selected</span>
      </div>
      <div className="flex flex-wrap gap-1.5 rounded-md border border-input bg-transparent p-2 min-h-[38px]">
        {(value ?? []).map((v) => (
          <span
            key={String(v)}
            className="inline-flex items-center gap-1 rounded-md border border-violet-500/30 bg-violet-500/10 px-2 py-0.5 text-[11px] text-violet-700 dark:text-violet-300"
          >
            {labelOf(v)}
            <button
              type="button"
              className="text-violet-500/70 hover:text-destructive"
              onClick={() => toggle(v)}
              title="Remove"
            >
              ×
            </button>
          </span>
        ))}
        {(value ?? []).length === 0 && (
          <span className="text-[11px] text-muted-foreground italic">Belum ada relasi dipilih</span>
        )}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="ml-auto h-7 gap-1 text-[11px]"
          onClick={() => setOpen(true)}
        >
          Pilih…
        </Button>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Select {col.label}</DialogTitle>
            <DialogDescription className="text-xs">
              Multi-select; picked records are linked via the junction table on save.
            </DialogDescription>
          </DialogHeader>
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              autoFocus
              placeholder="Search related records..."
              className="h-9 pl-8 text-xs"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="max-h-72 space-y-1 overflow-y-auto">
            {opts.isLoading && <p className="py-6 text-center text-xs text-muted-foreground">Loading...</p>}
            {(opts.data?.rows ?? []).map((row) => {
              const refV = row[targetRef];
              const on = selectedKeys.has(String(refV));
              const parts = (col.m2mDisplayColumns ?? [])
                .map((f) => (row[f] === null || row[f] === undefined ? null : String(row[f])))
                .filter((p): p is string => p !== null);
              return (
                <button
                  key={String(refV)}
                  type="button"
                  onClick={() => toggle(refV)}
                  className={`flex w-full items-center gap-2 rounded-md border px-3 py-2 text-left text-xs transition-colors ${
                    on ? "border-violet-500/40 bg-violet-500/10" : "hover:bg-muted/50"
                  }`}
                >
                  <span className="pointer-events-none"><Checkbox checked={on} readOnly tabIndex={-1} aria-hidden /></span>
                  <span className="flex-1 font-sans">{parts.join(" — ") || String(refV)}</span>
                  <Badge variant="outline" className="font-mono text-[10px]">{String(refV)}</Badge>
                </button>
              );
            })}
            {(opts.data?.rows ?? []).length === 0 && !opts.isLoading && (
              <p className="py-6 text-center text-xs text-muted-foreground">No records found</p>
            )}
          </div>
          <div className="flex items-center justify-between border-t pt-3">
            <span className="text-[11px] text-muted-foreground">
              {opts.data?.total ?? 0} records • page {opts.data?.page ?? 1}
            </span>
            <div className="flex gap-1">
              <Button variant="outline" size="sm" className="h-7 text-xs" disabled={(opts.data?.page ?? 1) <= 1} onClick={() => setPage((p) => p - 1)}>
                <ChevronLeft className="h-3.5 w-3.5" />
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                disabled={(opts.data?.page ?? 1) >= Math.ceil((opts.data?.total ?? 0) / (opts.data?.pageSize ?? 20))}
                onClick={() => setPage((p) => p + 1)}
              >
                <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
