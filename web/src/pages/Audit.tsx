import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { AuditEntry, TableDef } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default function Audit() {
  const [tableDefId, setTableDefId] = useState("");
  const [action, setAction] = useState("");
  const [page, setPage] = useState(1);
  const defs = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const q = new URLSearchParams();
  if (tableDefId) q.set("tableDefId", tableDefId);
  if (action) q.set("action", action);
  q.set("page", String(page));
  const audit = useQuery({
    queryKey: ["audit", tableDefId, action, page],
    queryFn: () => api<{ entries: AuditEntry[]; total: number; page: number; pageSize: number }>(`/audit?${q}`),
  });
  const pages = audit.data ? Math.max(1, Math.ceil(audit.data.total / audit.data.pageSize)) : 1;
  const defName = (id: number) => defs.data?.find((d) => d.id === id)?.label ?? `#${id}`;

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold">Audit trail</h1>
      <div className="flex gap-4">
        <div className="space-y-1">
          <Label>Table</Label>
          <Select value={tableDefId || "all"} onValueChange={(v) => { setTableDefId(v === "all" ? "" : v); setPage(1); }}>
            <SelectTrigger className="w-48"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">all tables</SelectItem>
              {(defs.data ?? []).map((d) => <SelectItem key={d.id} value={String(d.id)}>{d.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <Label>Action</Label>
          <Select value={action || "all"} onValueChange={(v) => { setAction(v === "all" ? "" : v); setPage(1); }}>
            <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">all</SelectItem>
              {["INSERT", "UPDATE", "DELETE"].map((a) => <SelectItem key={a} value={a}>{a}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      </div>
      <Table>
        <TableHeader><TableRow>
          <TableHead>When</TableHead><TableHead>Table</TableHead><TableHead>Action</TableHead>
          <TableHead>Row PK</TableHead><TableHead>Changes</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {(audit.data?.entries ?? []).map((e) => (
            <TableRow key={e.id}>
              <TableCell className="whitespace-nowrap text-xs">{e.createdAt}</TableCell>
              <TableCell>{defName(e.tableDefId)}</TableCell>
              <TableCell>
                <Badge variant={e.action === "DELETE" ? "destructive" : "secondary"}>{e.action}</Badge>
              </TableCell>
              <TableCell className="font-mono text-xs">{e.rowPk}</TableCell>
              <TableCell>
                <details>
                  <summary className="cursor-pointer text-xs text-muted-foreground">diff</summary>
                  <pre className="max-w-xl overflow-x-auto text-[10px]">
                    {JSON.stringify({ old: e.oldValues, new: e.newValues }, null, 1)}
                  </pre>
                </details>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Prev</Button>
        page {page} / {pages} — {audit.data?.total ?? 0} entries
        <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</Button>
      </div>
    </div>
  );
}
