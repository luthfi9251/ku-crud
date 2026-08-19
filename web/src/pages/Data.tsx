import { useEffect, useMemo, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Search,
  Plus,
  RefreshCw,
  Edit,
  Trash2,
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
import type { ColumnDef, Row, RowsRes, TableDefPayload } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Card, CardContent } from "@/components/ui/card";

export default function Data() {
  const { id } = useParams();
  const qc = useQueryClient();
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [sort, setSort] = useState("");
  const [dir, setDir] = useState<"ASC" | "DESC">("ASC");
  const [page, setPage] = useState(1);
  const [drift, setDrift] = useState<{ missing: string[]; added: string[]; typeChanged: string[] } | null>(null);
  const [connErr, setConnErr] = useState("");
  const [form, setForm] = useState<{ mode: "new" | "edit"; row: Row } | null>(null);

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
  const pkCol = def.data?.pkColumn ?? "";

  const save = useMutation({
    mutationFn: async () => {
      const body = JSON.stringify(form!.row);
      if (form!.mode === "new") await api(`/tables/${id}/rows`, { method: "POST", body });
      else await api(`/tables/${id}/rows/${encodeURIComponent(String(form!.row[pkCol]))}`, { method: "PUT", body });
    },
    onSuccess: () => {
      setForm(null);
      rows.refetch();
    },
  });

  const del = useMutation({
    mutationFn: (pk: unknown) => api(`/tables/${id}/rows/${encodeURIComponent(String(pk))}`, { method: "DELETE" }),
    onSuccess: () => rows.refetch(),
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
              PK: <span className="font-mono text-foreground font-semibold">{pkCol}</span> &bull; {r?.total ?? 0} total records
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
          <Button
            onClick={() => setForm({ mode: "new", row: {} })}
            className="h-9 bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1 text-xs"
          >
            <Plus className="h-4 w-4" /> New Row
          </Button>
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
                    <TableRow key={String(row[pkCol]) + i} className="hover:bg-muted/20">
                      {cols.map((c) => (
                        <TableCell key={c.name} className="text-xs font-mono max-w-xs truncate">
                          {renderValue(row[c.name], c.fieldType)}
                        </TableCell>
                      ))}
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-muted-foreground hover:text-foreground"
                            onClick={() => setForm({ mode: "edit", row: { ...row } })}
                            title="Edit row"
                          >
                            <Edit className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-muted-foreground hover:text-destructive"
                            onClick={() => {
                              if (confirm("Delete this record permanently?")) del.mutate(row[pkCol]);
                            }}
                            title="Delete row"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
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
            <DialogTitle>{form?.mode === "new" ? "Insert New Record" : "Edit Record"}</DialogTitle>
            <DialogDescription className="text-xs">
              {form?.mode === "new" ? "Provide values for editable table fields" : `Editing record PK #${form?.row[pkCol]}`}
            </DialogDescription>
          </DialogHeader>

          {form && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 py-2">
              {editable.map((c) => (
                <FieldInput
                  key={c.name}
                  col={c}
                  row={form.row}
                  onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })}
                />
              ))}
            </div>
          )}

          {save.isError && (
            <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive">
              {(save.error as Error).message}: {String((save.error as ApiError).detail ?? "")}
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0">
            <Button variant="outline" onClick={() => setForm(null)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                const missing = editable.filter(
                  (c) => c.required && (form!.row[c.name] === undefined || form!.row[c.name] === null || form!.row[c.name] === "")
                );
                if (missing.length) {
                  return alert(`Required fields missing: ${missing.map((c) => c.label).join(", ")}`);
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
  return String(v);
}

function FieldInput({ col, row, onChange }: { col: ColumnDef; row: Row; onChange: (v: unknown) => void }) {
  const raw = row[col.name];
  const val = raw === null || raw === undefined ? "" : String(raw);

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <Label className="text-xs font-medium">
          {col.label} {col.required && <span className="text-destructive">*</span>}
        </Label>
        <span className="text-[10px] text-muted-foreground font-mono">{col.fieldType}</span>
      </div>

      {col.fieldType === "boolean" ? (
        <div className="flex items-center gap-2 pt-1">
          <Switch checked={raw === true} onCheckedChange={onChange} />
          <span className="text-xs font-mono">{raw === true ? "TRUE" : "FALSE"}</span>
        </div>
      ) : col.fieldType === "enum" ? (
        <Select value={val || undefined} onValueChange={onChange}>
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
          className="h-9 text-xs"
          value={val.slice(0, 16)}
          onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)}
        />
      ) : col.fieldType === "number" ? (
        <Input
          type="number"
          className="h-9 text-xs"
          value={val}
          onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
        />
      ) : (
        <Input className="h-9 text-xs" value={val} onChange={(e) => onChange(e.target.value)} />
      )}
    </div>
  );
}
