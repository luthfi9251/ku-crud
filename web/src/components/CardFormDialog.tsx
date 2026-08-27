import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import type { AggFunc, ColumnDef, StatCard, TableDef, TableDefPayload } from "@/lib/types";
import { FilterBar, deserializeFilters, serializeFilters, type ActiveFilter } from "@/components/FilterBar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const FUNCS: AggFunc[] = ["count", "sum", "avg", "min", "max"];

function colsForFunc(cols: ColumnDef[], fn: AggFunc): ColumnDef[] {
  const real = cols.filter((c) => !c.isComputed && c.fieldType !== "m2m" && c.fieldType !== "fk");
  if (fn === "count") return [];
  if (fn === "sum" || fn === "avg") return real.filter((c) => c.fieldType === "number");
  return real.filter((c) => c.fieldType === "number" || c.fieldType === "datetime");
}

export function CardFormDialog({ open, onClose, card }: {
  open: boolean;
  onClose: () => void;
  card?: StatCard | null; // null/undefined = create
}) {
  const t = useT();
  const qc = useQueryClient();
  const tables = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const [tableId, setTableId] = useState("");
  const [label, setLabel] = useState("");
  const [fn, setFn] = useState<AggFunc>("count");
  const [column, setColumn] = useState("");
  const [filters, setFilters] = useState<ActiveFilter[]>([]);

  useEffect(() => {
    if (!open) return;
    if (card) {
      setTableId(card.tableDefId);
      setLabel(card.label);
      setFn(card.func);
      setColumn(card.column);
      setFilters(deserializeFilters(card.filters));
    } else {
      setTableId("");
      setLabel("");
      setFn("count");
      setColumn("");
      setFilters([]);
    }
  }, [open, card]);

  const def = useQuery({
    queryKey: ["cardformdef", tableId],
    enabled: !!tableId,
    queryFn: () => api<TableDefPayload>(`/tables/${tableId}`),
  });
  const allCols = def.data?.columns ?? [];
  const eligible = colsForFunc(allCols, fn);

  // drop column/filters that stop being valid when table or func changes
  useEffect(() => {
    if (column && !eligible.some((c) => c.name === column)) setColumn("");
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [tableId, fn, def.data]);
  useEffect(() => {
    const names = new Set(allCols.map((c) => c.name));
    setFilters((fs) => fs.filter((f) => names.has(f.column)));
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [tableId]);

  const save = useMutation({
    mutationFn: () => {
      const body = {
        tableDefId: tableId,
        label: label.trim(),
        func: fn,
        column: fn === "count" ? "" : column,
        filters: serializeFilters(filters) || "[]",
      };
      return api(card ? `/cards/${card.id}` : "/cards", { method: card ? "PUT" : "POST", body: JSON.stringify(body) });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cards"] });
      onClose();
    },
  });

  const canSave = !!tableId && label.trim().length > 0 && (fn === "count" || !!column);

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{card ? t("card.edit") : t("dash.addCard")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label className="text-xs">{t("card.table")}</Label>
            <Select value={tableId || undefined} onValueChange={(v) => { setTableId(v); setColumn(""); setFilters([]); }}>
              <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {(tables.data ?? []).map((tb) => (
                  <SelectItem key={tb.id} value={tb.id} className="text-xs">{tb.label || tb.tableName}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">{t("card.label")}</Label>
            <Input className="h-9 text-xs" value={label} placeholder={t("card.labelPh")} onChange={(e) => setLabel(e.target.value)} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label className="text-xs">{t("card.func")}</Label>
              <Select value={fn} onValueChange={(v) => setFn(v as AggFunc)}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {FUNCS.map((f) => <SelectItem key={f} value={f} className="text-xs">{t(`card.func.${f}`)}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("card.column")}</Label>
              <Select value={column || undefined} onValueChange={setColumn} disabled={fn === "count" || eligible.length === 0}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {eligible.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs">{c.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">{t("card.filters")}</Label>
            {allCols.length > 0 ? (
              <FilterBar cols={allCols} filters={filters} onChange={setFilters} />
            ) : (
              <p className="text-xs text-muted-foreground">{t("card.noTables")}</p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button onClick={() => save.mutate()} disabled={!canSave || save.isPending} className="bg-blue-600 text-white hover:bg-blue-700">
            {save.isPending ? t("form.saving") : t("card.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
