import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../lib/api";
import type { ColumnDef, Row, RowsRes, TableDefPayload } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export default function Data() {
  const { id } = useParams();
  const qc = useQueryClient();
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [sort, setSort] = useState("");
  const [dir, setDir] = useState("ASC");
  const [page, setPage] = useState(1);
  const [drift, setDrift] = useState<{ missing: string[]; added: string[]; typeChanged: string[] } | null>(null);
  const [connErr, setConnErr] = useState("");
  const [form, setForm] = useState<{ mode: "new" | "edit"; row: Row } | null>(null);

  useEffect(() => {
    const t = setTimeout(() => { setDebounced(search); setPage(1); }, 300);
    return () => clearTimeout(t);
  }, [search]);

  const verify = useMutation({
    mutationFn: async () => {
      try {
        await api(`/tables/${id}/verify`);
        setDrift(null); setConnErr("");
      } catch (e) {
        if (e instanceof ApiError && e.code === "DRIFT") setDrift(e.detail as never);
        else if (e instanceof ApiError && e.code === "CONN") setConnErr(e.message + ": " + String(e.detail ?? ""));
        else throw e;
      }
    },
  });
  useEffect(() => { verify.mutate(); /* eslint-disable-line */ }, [id]); // drift check on page visit only — spec

  const rows = useQuery({
    queryKey: ["rows", id, debounced, sort, dir, page],
    enabled: !!def.data,
    queryFn: () => {
      const p = new URLSearchParams();
      if (debounced) p.set("search", debounced);
      if (sort) { p.set("sort", sort); p.set("dir", dir); }
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
    onSuccess: () => { setForm(null); rows.refetch(); },
  });
  const del = useMutation({
    mutationFn: (pk: unknown) => api(`/tables/${id}/rows/${encodeURIComponent(String(pk))}`, { method: "DELETE" }),
    onSuccess: () => rows.refetch(),
  });
  const resync = useMutation({
    mutationFn: () => api(`/tables/${id}/resync`, { method: "POST" }),
    onSuccess: () => { setDrift(null); qc.invalidateQueries({ queryKey: ["def", id] }); rows.refetch(); },
  });

  if (def.isLoading) return <p>loading…</p>;
  if (!def.data) return <p className="text-destructive">table not found</p>;
  const r = rows.data;
  const pages = r ? Math.max(1, Math.ceil(r.total / r.pageSize)) : 1;

  // ponytail: plain table, TanStack Table when column resize/virtualization needed
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">{def.data.label}
          <span className="ml-2 text-sm text-muted-foreground">{def.data.schemaName}.{def.data.tableName}</span>
        </h1>
        <Button onClick={() => setForm({ mode: "new", row: {} })}>New row</Button>
      </div>

      {connErr && <div className="rounded border border-destructive bg-destructive/10 p-3 text-sm">{connErr}</div>}
      {drift && (
        <div className="rounded border border-destructive bg-destructive/10 p-3 text-sm">
          <b>Schema drift detected.</b>{" "}
          {["missing", "added", "typeChanged"].map((k) => {
            const v = (drift as never as Record<string, string[]>)[k] ?? [];
            return v.length ? <span key={k}> {k}: {v.join(", ")}. </span> : null;
          })}
          <Button variant="outline" size="sm" className="ml-2" onClick={() => resync.mutate()} disabled={resync.isPending}>Re-sync</Button>
        </div>
      )}

      {cols.some((c) => c.searchable) && (
        <Input placeholder="search…" className="max-w-xs" value={search} onChange={(e) => setSearch(e.target.value)} />
      )}

      <Table>
        <TableHeader><TableRow>
          {cols.map((c) => (
            <TableHead key={c.name} className={c.sortable ? "cursor-pointer select-none" : ""} onClick={() => {
              if (!c.sortable) return;
              if (sort === c.name) setDir(dir === "ASC" ? "DESC" : "ASC");
              else { setSort(c.name); setDir("ASC"); }
              setPage(1);
            }}>
              {c.label}{sort === c.name ? (dir === "ASC" ? " ▲" : " ▼") : ""}
            </TableHead>
          ))}
          <TableHead></TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {(r?.rows ?? []).map((row, i) => (
            <TableRow key={String(row[pkCol]) + i}>
              {cols.map((c) => <TableCell key={c.name}>{render(row[c.name])}</TableCell>)}
              <TableCell className="space-x-2 text-right">
                <Button variant="outline" size="sm" onClick={() => setForm({ mode: "edit", row: { ...row } })}>Edit</Button>
                <Button variant="outline" size="sm" onClick={() => {
                  if (confirm("Delete this row?")) del.mutate(row[pkCol]);
                }}>Delete</Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Prev</Button>
        page {page} / {pages} — {r?.total ?? 0} rows
        <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</Button>
      </div>

      <Dialog open={!!form} onOpenChange={(o) => !o && setForm(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>{form?.mode === "new" ? "New row" : "Edit row"}</DialogTitle></DialogHeader>
          {form && (
            <div className="grid gap-3">
              {editable.map((c) => <FieldInput key={c.name} col={c} row={form.row}
                onChange={(v) => setForm({ ...form, row: { ...form.row, [c.name]: v } })} />)}
            </div>
          )}
          {save.isError && <p className="text-sm text-destructive">{(save.error as Error).message}: {String((save.error as ApiError).detail ?? "")}</p>}
          <DialogFooter>
            <Button onClick={() => {
              const missing = editable.filter((c) => c.required && (form!.row[c.name] === undefined || form!.row[c.name] === null || form!.row[c.name] === ""));
              if (missing.length) return alert(`required: ${missing.map((c) => c.label).join(", ")}`);
              save.mutate();
            }} disabled={save.isPending}>Save</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function render(v: unknown): string {
  if (v === null || v === undefined) return "—";
  if (typeof v === "boolean") return v ? "true" : "false";
  return String(v);
}

function FieldInput({ col, row, onChange }: { col: ColumnDef; row: Row; onChange: (v: unknown) => void }) {
  const raw = row[col.name];
  const val = raw === null || raw === undefined ? "" : String(raw);
  return (
    <div className="space-y-1">
      <Label>{col.label}{col.required && " *"}</Label>
      {col.fieldType === "boolean" ? (
        <Switch checked={raw === true} onCheckedChange={onChange} />
      ) : col.fieldType === "enum" ? (
        <Select value={val || undefined} onValueChange={onChange}>
          <SelectTrigger><SelectValue placeholder="—" /></SelectTrigger>
          <SelectContent>
            {(col.enumOptions ?? []).map((o) => <SelectItem key={o} value={o}>{o}</SelectItem>)}
          </SelectContent>
        </Select>
      ) : col.fieldType === "datetime" ? (
        <Input type="datetime-local" value={val.slice(0, 16)} onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)} />
      ) : col.fieldType === "number" ? (
        <Input type="number" value={val} onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))} />
      ) : (
        <Input value={val} onChange={(e) => onChange(e.target.value)} />
      )}
      <Badge variant="outline" className="text-[10px]">{col.fieldType}</Badge>
    </div>
  );
}
