import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Inbox, Filter, ChevronLeft, ChevronRight, RefreshCw, RotateCcw, Clock, CheckCircle2, XCircle, Calendar } from "lucide-react";
import { api } from "../lib/api";
import type { OutboxEntry, OutboxListRes } from "../lib/types";
import { useT } from "../lib/i18n";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent } from "@/components/ui/card";

const PAGE_SIZE = 50;

export default function HooksOutbox() {
  const t = useT();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);

  const q = new URLSearchParams();
  if (status) q.set("status", status);
  q.set("page", String(page));

  const outbox = useQuery({
    queryKey: ["outbox", status, page],
    queryFn: () => api<OutboxListRes>(`/hooks/outbox?${q}`),
  });

  const retry = async (id: string) => {
    try {
      await api(`/hooks/outbox/${id}/retry`, { method: "POST" });
      alert(t("ob.retryOk"));
      queryClient.invalidateQueries({ queryKey: ["outbox"] });
    } catch {
      // api() already redirects on auth failure; ignore other errors
    }
  };

  const pages = outbox.data ? Math.max(1, Math.ceil(outbox.data.total / PAGE_SIZE)) : 1;

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between border-b pb-4">
        <div>
          <h2 className="text-xl font-bold tracking-tight">{t("ob.title")}</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t("ob.hint")}
          </p>
        </div>

        {/* Filter Bar */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <Select
              value={status || "all"}
              onValueChange={(v) => {
                setStatus(v === "all" ? "" : v);
                setPage(1);
              }}
            >
              <SelectTrigger className="h-9 w-36 text-xs">
                <SelectValue placeholder={t("ob.filter.all")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all" className="text-xs">{t("ob.filter.all")}</SelectItem>
                <SelectItem value="pending" className="text-xs">{t("ob.status.pending")}</SelectItem>
                <SelectItem value="done" className="text-xs">{t("ob.status.done")}</SelectItem>
                <SelectItem value="dead" className="text-xs">{t("ob.status.dead")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-9 gap-1 text-xs"
            onClick={() => outbox.refetch()}
          >
            <RefreshCw className="h-3.5 w-3.5" /> {t("ob.refresh")}
          </Button>
        </div>
      </div>

      {/* Main Outbox Table Card */}
      <Card className="border-border/60 shadow-sm overflow-hidden">
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/50 hover:bg-muted/50">
                <TableHead className="w-48">{t("ob.col.created")}</TableHead>
                <TableHead className="w-28">{t("ob.col.event")}</TableHead>
                <TableHead>{t("ob.col.hook")}</TableHead>
                <TableHead className="w-28">{t("ob.col.status")}</TableHead>
                <TableHead className="w-24">{t("ob.col.attempts")}</TableHead>
                <TableHead>{t("ob.col.lastError")}</TableHead>
                <TableHead className="w-24 text-right">{t("ob.retry")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {!outbox.isLoading && (outbox.data?.entries ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="h-32 text-center">
                    <div className="flex flex-col items-center justify-center space-y-1">
                      <Inbox className="h-7 w-7 text-muted-foreground/30" />
                      <p className="text-xs font-medium text-muted-foreground">{t("ob.empty")}</p>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                (outbox.data?.entries ?? []).map((e: OutboxEntry) => (
                  <TableRow key={e.id} className="hover:bg-muted/20">
                    <TableCell className="whitespace-nowrap text-xs font-mono text-muted-foreground">
                      <div className="flex items-center gap-1.5">
                        <Calendar className="h-3.5 w-3.5 text-muted-foreground/60" />
                        {e.createdAt}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs font-semibold text-foreground">
                      {e.event}
                    </TableCell>
                    <TableCell className="font-mono text-xs font-medium">
                      {e.hookName}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={e.status} label={t(`ob.status.${e.status}`)} />
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {e.attempts}
                    </TableCell>
                    <TableCell className="max-w-[24rem] truncate text-xs text-muted-foreground" title={e.lastError}>
                      {e.lastError || "—"}
                    </TableCell>
                    <TableCell className="text-right">
                      {e.status !== "done" && (
                        <Button
                          variant="secondary"
                          size="sm"
                          className="h-7 text-xs gap-1"
                          onClick={() => retry(String(e.id))}
                        >
                          <RotateCcw className="h-3.5 w-3.5" /> {t("ob.retry")}
                        </Button>
                      )}
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
            {t("data.pageOf", { page: String(page), pages: String(pages) })} &bull; {t("audit.totalEntries", { count: String(outbox.data?.total ?? 0) })}
          </span>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1 text-xs"
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              <ChevronLeft className="h-3.5 w-3.5" /> {t("data.prev")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1 text-xs"
              disabled={page >= pages}
              onClick={() => setPage(page + 1)}
            >
              {t("data.next")} <ChevronRight className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}

function StatusBadge({ status, label }: { status: OutboxEntry["status"]; label: string }) {
  if (status === "pending") {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20 font-semibold">
        <Clock className="h-3 w-3" /> {label}
      </Badge>
    );
  }
  if (status === "done") {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20 font-semibold">
        <CheckCircle2 className="h-3 w-3" /> {label}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20 font-semibold">
      <XCircle className="h-3 w-3" /> {label}
    </Badge>
  );
}
