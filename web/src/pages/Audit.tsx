import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldCheck, Filter, ChevronLeft, ChevronRight, Eye, PlusCircle, Edit3, Trash2, Calendar, Database } from "lucide-react";
import { api } from "../lib/api";
import { displayRowPk } from "../lib/rowkey";
import type { AuditEntry, TableDef } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

export default function Audit() {
  const [tableDefId, setTableDefId] = useState("");
  const [action, setAction] = useState("");
  const [page, setPage] = useState(1);
  const [selectedEntry, setSelectedEntry] = useState<AuditEntry | null>(null);

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
  const defName = (id: string) => defs.data?.find((d) => d.id === id)?.label ?? "unknown table";

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between border-b pb-4">
        <div>
          <h2 className="text-xl font-bold tracking-tight">Audit Trail Logs</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Real-time track of all database mutations, row inserts, updates, and deletions
          </p>
        </div>

        {/* Filter Bar */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <Database className="h-4 w-4 text-muted-foreground" />
            <Select
              value={tableDefId || "all"}
              onValueChange={(v) => {
                setTableDefId(v === "all" ? "" : v);
                setPage(1);
              }}
            >
              <SelectTrigger className="h-9 w-44 text-xs">
                <SelectValue placeholder="All tables" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all" className="text-xs">All Tables</SelectItem>
                {(defs.data ?? []).map((d) => (
                  <SelectItem key={d.id} value={String(d.id)} className="text-xs">
                    {d.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center gap-1.5">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <Select
              value={action || "all"}
              onValueChange={(v) => {
                setAction(v === "all" ? "" : v);
                setPage(1);
              }}
            >
              <SelectTrigger className="h-9 w-32 text-xs">
                <SelectValue placeholder="All actions" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all" className="text-xs">All Actions</SelectItem>
                {["INSERT", "UPDATE", "DELETE"].map((a) => (
                  <SelectItem key={a} value={a} className="text-xs">
                    {a}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      {/* Main Audit Table Card */}
      <Card className="border-border/60 shadow-sm overflow-hidden">
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/50 hover:bg-muted/50">
                <TableHead className="w-48">Timestamp</TableHead>
                <TableHead>Target Table</TableHead>
                <TableHead className="w-32">User</TableHead>
                <TableHead className="w-28">Action</TableHead>
                <TableHead className="w-36">Row Key</TableHead>
                <TableHead className="text-right">Diff Inspector</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {audit.isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-xs text-muted-foreground">
                    Loading audit trail logs...
                  </TableCell>
                </TableRow>
              ) : (audit.data?.entries ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-32 text-center">
                    <div className="flex flex-col items-center justify-center space-y-1">
                      <ShieldCheck className="h-7 w-7 text-muted-foreground/30" />
                      <p className="text-xs font-medium text-muted-foreground">No audit entries found</p>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                (audit.data?.entries ?? []).map((e) => (
                  <TableRow key={e.id} className="hover:bg-muted/20">
                    <TableCell className="whitespace-nowrap text-xs font-mono text-muted-foreground">
                      <div className="flex items-center gap-1.5">
                        <Calendar className="h-3.5 w-3.5 text-muted-foreground/60" />
                        {e.createdAt}
                      </div>
                    </TableCell>
                    <TableCell className="font-semibold text-xs text-foreground">
                      {defName(e.tableDefId)}
                    </TableCell>
                    <TableCell className="text-xs font-mono text-muted-foreground">
                      {e.username || "—"}
                    </TableCell>
                    <TableCell>
                      <ActionBadge action={e.action} />
                    </TableCell>
                    <TableCell className="font-mono text-xs font-medium">
                      {displayRowPk(e.rowPk)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="secondary"
                        size="sm"
                        className="h-7 text-xs gap-1"
                        onClick={() => setSelectedEntry(e)}
                      >
                        <Eye className="h-3.5 w-3.5 text-blue-500" /> View Diff
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>

        {/* Footer Pagination */}
        <div className="flex items-center justify-between border-t bg-muted/20 px-4 py-3 text-xs text-muted-foreground">
          <span>
            Page <strong className="text-foreground">{page}</strong> of <strong className="text-foreground">{pages}</strong> &bull; Total {audit.data?.total ?? 0} log entries
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

      {/* Diff Inspector Modal */}
      {selectedEntry && (
        <Dialog open={!!selectedEntry} onOpenChange={(o) => !o && setSelectedEntry(null)}>
          <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
            <DialogHeader className="border-b pb-3">
              <div className="flex items-center justify-between">
                <DialogTitle className="text-base font-bold flex items-center gap-2">
                  <ShieldCheck className="h-5 w-5 text-blue-500" />
                  Audit Entry Log #{selectedEntry.id}
                </DialogTitle>
                <ActionBadge action={selectedEntry.action} />
              </div>
              <DialogDescription className="text-xs font-mono mt-1">
                Table: {defName(selectedEntry.tableDefId)} &bull; User: {selectedEntry.username} &bull; Row Key: {displayRowPk(selectedEntry.rowPk)} &bull; {selectedEntry.createdAt}
              </DialogDescription>
            </DialogHeader>

            <div className="grid grid-cols-2 gap-4 py-3">
              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-rose-600 dark:text-rose-400">Previous State (Old Values)</Label>
                <div className="rounded-lg border bg-muted/40 p-3 font-mono text-[11px] max-h-60 overflow-y-auto">
                  <pre className="whitespace-pre-wrap">
                    {selectedEntry.oldValues ? JSON.stringify(selectedEntry.oldValues, null, 2) : "null"}
                  </pre>
                </div>
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-emerald-600 dark:text-emerald-400">New State (New Values)</Label>
                <div className="rounded-lg border bg-muted/40 p-3 font-mono text-[11px] max-h-60 overflow-y-auto">
                  <pre className="whitespace-pre-wrap">
                    {selectedEntry.newValues ? JSON.stringify(selectedEntry.newValues, null, 2) : "null"}
                  </pre>
                </div>
              </div>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function ActionBadge({ action }: { action: string }) {
  if (action === "INSERT") {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20 font-semibold">
        <PlusCircle className="h-3 w-3" /> INSERT
      </Badge>
    );
  }
  if (action === "UPDATE") {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20 font-semibold">
        <Edit3 className="h-3 w-3" /> UPDATE
      </Badge>
    );
  }
  if (action === "DELETE") {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20 font-semibold">
        <Trash2 className="h-3 w-3" /> DELETE
      </Badge>
    );
  }
  return <Badge variant="outline" className="text-[10px]">{action}</Badge>;
}
