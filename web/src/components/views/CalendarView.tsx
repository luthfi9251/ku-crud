import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { api } from "../../lib/api";
import { serializeFilters, type ActiveFilter } from "../FilterBar";
import type { ColumnDef, Row, RowsRes, TableDefPayload } from "../../lib/types";
import { formatCell } from "../../lib/format";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// Month calendar over a datetime start column (optional end column spans
// events across days). All rows in the visible month are fetched page by
// page (between filter on the start column).
export function CalendarView({ def, startCol, endCol, filters, search, pageSize, lang, dataVersion, onEdit, onDayCreate }: {
  def: TableDefPayload; startCol: ColumnDef; endCol?: ColumnDef;
  filters: ActiveFilter[]; search: string; pageSize: number; lang: string;
  dataVersion?: number;
  onEdit: (row: Row) => void;
  onDayCreate: (date: string) => void;
}) {
  const [cursor, setCursor] = useState(() => { const d = new Date(); return { y: d.getFullYear(), m: d.getMonth() }; });
  const first = new Date(cursor.y, cursor.m, 1);
  const last = new Date(cursor.y, cursor.m + 1, 0);

  // helpers declared before the query that closes over them
  const fmt = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  const q2 = (base: URLSearchParams, page: number) => { const c = new URLSearchParams(base); c.set("page", String(page)); return c; };

  const events = useQuery({
    queryKey: ["cal", def.id, cursor.y, cursor.m, search, filters, dataVersion],
    queryFn: async () => {
      const fs = serializeFilters([
        ...filters,
        { column: startCol.name, op: "between", values: [fmt(first), fmt(last)] },
      ]);
      const q = new URLSearchParams();
      if (fs) q.set("filters", fs);
      if (search) q.set("search", search);
      q.set("page", "1"); q.set("limit", String(pageSize));
      const res = await api<RowsRes>(`/tables/${def.id}/rows?${q}`); // seed fetch — page 1
      const out: Row[] = [...res.rows];
      for (let page = 2; out.length < res.total; page++) {
        const cur = await api<RowsRes>(`/tables/${def.id}/rows?${q2(q, page)}`);
        out.push(...cur.rows);
        if (cur.rows.length === 0) break;
      }
      return out;
    },
  });

  const startOffset = (first.getDay() + 6) % 7; // Monday-first
  const cells: (Date | null)[] = [];
  for (let i = 0; i < startOffset; i++) cells.push(null);
  for (let d = 1; d <= last.getDate(); d++) cells.push(new Date(cursor.y, cursor.m, d));

  const eventsByDate = useMemo(() => {
    const monthEnd = new Date(cursor.y, cursor.m + 1, 0);
    const map: Record<string, Row[]> = {};
    for (const row of events.data ?? []) {
      const start = new Date(String(row[startCol.name]));
      if (isNaN(start.getTime())) continue;
      const end = endCol && row[endCol.name] != null ? new Date(String(row[endCol.name])) : new Date(start);
      const keyOf = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      for (let d = new Date(start); d <= end && d <= monthEnd; d = new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1)) {
        const k = keyOf(d);
        (map[k] = map[k] ?? []).push(row);
      }
    }
    return map;
  }, [events.data, startCol.name, endCol, cursor]);

  const title = (row: Row) => {
    const c = def.columns.find((x) => x.visible && x.name !== startCol.name && !x.isComputed);
    return c ? formatCell(c, row[c.name], lang) : String(row[startCol.name]);
  };

  // keep the cursor month in 0..11 so MONTHS[...] and Date arithmetic stay valid
  const moveMonth = (delta: number) =>
    setCursor((c) => { const d = new Date(c.y, c.m + delta, 1); return { y: d.getFullYear(), m: d.getMonth() }; });

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex items-center justify-between border-b px-4 py-2">
        <h3 className="text-sm font-semibold">{MONTHS[cursor.m]} {cursor.y}</h3>
        <div className="flex items-center gap-1">
          <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => moveMonth(-1)}><ChevronLeft className="h-3.5 w-3.5" /></Button>
          <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => { const d = new Date(); setCursor({ y: d.getFullYear(), m: d.getMonth() }); }}>Today</Button>
          <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => moveMonth(1)}><ChevronRight className="h-3.5 w-3.5" /></Button>
        </div>
      </div>
      <div className="grid grid-cols-7 text-center text-[10px] font-medium text-muted-foreground border-b py-1.5">
        {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) => <span key={d}>{d}</span>)}
      </div>
      <div className="grid grid-cols-7">
        {cells.map((d, i) => {
          if (!d) return <div key={i} className="min-h-20 border border-border/40 bg-muted/10" />;
          const k = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
          const dayEvents = eventsByDate[k] ?? [];
          const isToday = d.toDateString() === new Date().toDateString();
          return (
            <div key={i} onClick={() => onDayCreate(`${k}T00:00`)}
              className={cn("min-h-20 cursor-pointer border border-border/40 p-1 text-left", isToday && "bg-blue-500/5")}>
              <span className={cn("inline-flex h-5 w-5 items-center justify-center rounded-full text-[10px]", isToday && "bg-blue-600 text-white")}>{d.getDate()}</span>
              <div className="mt-0.5 space-y-0.5">
                {dayEvents.slice(0, 3).map((row, j) => (
                  <button key={j} onClick={(e) => { e.stopPropagation(); onEdit(row); }}
                    className="block w-full truncate rounded bg-blue-500/15 px-1 py-0.5 text-[9px] text-blue-700 dark:text-blue-300 hover:bg-blue-500/25">
                    {title(row)}
                  </button>
                ))}
                {dayEvents.length > 3 && <p className="text-[9px] text-muted-foreground">+{dayEvents.length - 3} more</p>}
              </div>
            </div>
          );
        })}
      </div>
      {events.isFetching && <p className="border-t px-4 py-1.5 text-[10px] text-muted-foreground">Loading events…</p>}
    </div>
  );
}
