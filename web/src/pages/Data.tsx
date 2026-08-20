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
} from "lucide-react";
import { api, ApiError } from "../lib/api";
import { encodeRowKey } from "../lib/rowkey";
import type { ColumnDef, FkOptionsRes, Row, RowsRes, TableDefPayload } from "../lib/types";
import { HelpPopover } from "../components/ColumnListEditor";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Card, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";

export default function Data() {
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const autoEditParam = searchParams.get("autoEdit");
  const searchParam = searchParams.get("search") || "";

  const qc = useQueryClient();
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  const [search, setSearch] = useState(searchParam);
  const [debounced, setDebounced] = useState(searchParam);
  const [sort, setSort] = useState("");
  const [dir, setDir] = useState<"ASC" | "DESC">("ASC");
  const [page, setPage] = useState(1);
  const [drift, setDrift] = useState<{ missing: string[]; added: string[]; typeChanged: string[] } | null>(null);
  const [connErr, setConnErr] = useState("");
  const [form, setForm] = useState<{ mode: "new" | "edit"; row: Row; initialKey?: string[] | null } | null>(null);

  // Sync search & table state when table id or URL search changes
  useEffect(() => {
    setSearch(searchParam);
    setDebounced(searchParam);
    setPage(1);
    setSort("");
    setDir("ASC");
    setForm(null);
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
    queryKey: ["rows", id, debounced, sort, dir, page],
    enabled: !!def.data,
    queryFn: () => {
      const p = new URLSearchParams();
      if (debounced) p.set("search", debounced);
      if (sort) {
        p.set("sort", sort);
        p.set("dir", dir);
      }
      p.set("page", String(page));
      return api<RowsRes>(`/tables/${id}/rows?${p}`);
    },
  });

  const cols = useMemo(() => (def.data?.columns ?? []).filter((c) => c.visible), [def.data]);
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
    return allCols.filter((c) => c.editable || keyCols.includes(c.name));
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
        for (const c of editable.filter((c) => !keyCols.includes(c.name))) {
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
    },
  });

  const [delErr, setDelErr] = useState("");
  const del = useMutation({
    mutationFn: (key: string[]) => api(`/tables/${id}/rows/${encodeRowKey(key)}`, { method: "DELETE" }),
    onSuccess: () => {
      setDelErr("");
      rows.refetch();
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

  const resync = useMutation({
    mutationFn: () => api(`/tables/${id}/resync`, { method: "POST" }),
    onSuccess: () => {
      setDrift(null);
      qc.invalidateQueries({ queryKey: ["def", id] });
      rows.refetch();
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
  const pages = r ? Math.max(1, Math.ceil(r.total / r.pageSize)) : 1;

  const rels = rows.data?.rels;
  const fkDisplay = (c: ColumnDef, row: Row): string | null => {
    if (c.fieldType !== "fk" || !c.fkDisplayColumns) return null;
    const rel = rels?.[c.name]?.[String(row[c.name])];
    if (!rel) return null;
    const parts = c.fkDisplayColumns
      .map((f) => (rel[f] === null || rel[f] === undefined ? null : String(rel[f])))
      .filter((p): p is string => p !== null);
    return parts.length ? parts.join(" — ") : null;
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

        <div className="flex items-center gap-2">
          {cols.some((c) => c.searchable) && (
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search rows..."
                className="h-9 w-48 pl-8 text-xs md:w-64"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </div>
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
          {perms.create && (
            <Button
              onClick={() => setForm({ mode: "new", row: {} })}
              className="h-9 bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1 text-xs"
            >
              <Plus className="h-4 w-4" /> New Row
            </Button>
          )}
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

      {/* Main Data Table */}
      <Card className="border-border/60 shadow-sm overflow-hidden">
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/50 hover:bg-muted/50">
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
                {rows.isLoading ? (
                  <TableRow>
                    <TableCell colSpan={cols.length + 1} className="h-24 text-center text-xs text-muted-foreground">
                      Fetching records...
                    </TableCell>
                  </TableRow>
                ) : (r?.rows ?? []).length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={cols.length + 1} className="h-32 text-center">
                      <div className="flex flex-col items-center justify-center space-y-1">
                        <Database className="h-7 w-7 text-muted-foreground/30" />
                        <p className="text-xs font-medium text-muted-foreground">No records found</p>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : (
                  (r?.rows ?? []).map((row, i) => (
                    <TableRow key={(rowKey(row) ?? []).join("\u0000") + i} className="hover:bg-muted/20">
                      {cols.map((c) => {
                        const disp = fkDisplay(c, row);
                        return (
                          <TableCell
                            key={c.name}
                            className={`text-xs font-mono max-w-xs ${c.fieldType === "json" ? "align-top" : "truncate"}`}
                          >
                            {disp !== null ? (
                              <span className="font-sans">{disp}</span>
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
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>

        {/* Footer Pagination */}
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
      </Card>

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
                return c.fieldType === "fk" ? (
                  <FkField
                    key={c.name}
                    col={c}
                    tableId={id}
                    row={form.row}
                    rels={rows.data?.rels}
                    onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })}
                  />
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
