import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Pencil, Trash2 } from "lucide-react";
import { api } from "@/lib/api";
import { formatCell } from "@/lib/format";
import { useI18nLang, useT } from "@/lib/i18n";
import type { ColumnDef, StatCard, StatsResult, TableDefPayload } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

function statsPath(dataName: string, c: StatCard): string {
  const p = new URLSearchParams();
  p.set("func", c.func);
  if (c.column) p.set("column", c.column);
  if (c.filters && c.filters !== "[]") p.set("filters", c.filters);
  return `/data/${encodeURIComponent(dataName)}/stats?${p}`;
}

function renderCardValue(res: StatsResult | undefined, col: ColumnDef | undefined, lang: string): string {
  if (!res) return "…";
  if (res.value === null || res.value === undefined) return "—";
  if (col && typeof res.value === "number" && col.fieldType === "number") {
    return formatCell(col, res.value, lang);
  }
  if (col && col.fieldType === "datetime" && typeof res.value === "string") {
    const d = new Date(res.value);
    if (!isNaN(d.getTime())) {
      return new Intl.DateTimeFormat(lang === "id" ? "id-ID" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(d);
    }
  }
  return String(res.value);
}

export function StatCardView({ card, compact = false, onEdit, onDelete, onMove }: {
  card: StatCard;
  compact?: boolean;
  onEdit?: () => void;
  onDelete?: () => void;
  onMove?: (up: boolean) => void;
}) {
  const t = useT();
  const { lang } = useI18nLang();
  // query views have no table name — they are addressed by their masked def token
  const dataName = card.tableName || card.tableDefId;
  const def = useQuery({
    queryKey: ["carddef", card.tableDefId],
    queryFn: () => api<TableDefPayload>(`/data/${encodeURIComponent(dataName)}`),
  });
  const stats = useQuery({
    queryKey: ["stats", card.id],
    queryFn: () => api<StatsResult>(statsPath(dataName, card)),
  });
  const col = def.data?.columns.find((c) => c.name === card.column);
  const value = renderCardValue(stats.data, col, lang);

  if (compact) {
    return (
      <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2">
        <div className="min-w-0">
          <div className="truncate text-[11px] text-muted-foreground">{card.label}</div>
          <div className="text-lg font-semibold leading-tight tabular-nums">
            {stats.isLoading ? t("card.loading") : value}
          </div>
        </div>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{card.label}</CardTitle>
        {(onEdit || onDelete || onMove) && (
          <div className="flex items-center gap-0.5">
            {onMove && (
              <>
                <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onMove(true)} title={t("card.moveUp")}>
                  <ArrowUp className="h-3.5 w-3.5" />
                </Button>
                <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onMove(false)} title={t("card.moveDown")}>
                  <ArrowDown className="h-3.5 w-3.5" />
                </Button>
              </>
            )}
            {onEdit && (
              <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onEdit}>
                <Pencil className="h-3.5 w-3.5" />
              </Button>
            )}
            {onDelete && (
              <Button variant="ghost" size="icon" className="h-6 w-6 text-destructive" onClick={onDelete}>
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        )}
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-bold tabular-nums">{stats.isLoading ? t("card.loading") : value}</div>
        <div className="mt-1 text-xs text-muted-foreground">
          {t(`card.func.${card.func}`)}
          {card.column ? ` · ${col?.label ?? card.column}` : ""} · {t("card.of", { table: card.tableLabel })}
        </div>
      </CardContent>
    </Card>
  );
}
