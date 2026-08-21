import { useState } from "react";
import { Filter, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { ColumnDef } from "../lib/types";
import { useT } from "../lib/i18n";

export type FilterOp = "eq" | "neq" | "gt" | "gte" | "lt" | "lte" | "between" | "in" | "contains";
export interface ActiveFilter { column: string; op: FilterOp; values: string[] }

const OPS_BY_TYPE: Record<string, FilterOp[]> = {
  text: ["eq", "neq", "contains", "in"],
  uuid: ["eq", "neq", "contains", "in"],
  number: ["eq", "neq", "gt", "gte", "lt", "lte", "between", "in"],
  datetime: ["eq", "gt", "lt", "between"],
  boolean: ["eq"],
  enum: ["eq", "neq", "in"],
  fk: ["contains", "eq"],
};
// symbols stay untranslated; word operators go through the dictionary
const OP_SYMBOL: Partial<Record<FilterOp, string>> = {
  eq: "=", neq: "≠", gt: ">", gte: "≥", lt: "<", lte: "≤",
};
function opLabel(t: (key: string) => string, op: FilterOp): string {
  if (op === "between") return t("filter.between");
  if (op === "in") return t("filter.inList");
  if (op === "contains") return t("filter.contains");
  return OP_SYMBOL[op] ?? op;
}
function opsFor(c: ColumnDef): FilterOp[] { return OPS_BY_TYPE[c.fieldType] ?? []; }
function needCount(op: FilterOp): number { return op === "between" ? 2 : op === "in" ? -1 : 1; }

export function serializeFilters(fs: ActiveFilter[]): string {
  return fs.length ? JSON.stringify(fs.map((f) => ({ column: f.column, op: f.op, values: f.values }))) : "";
}

export function deserializeFilters(s: string): ActiveFilter[] {
  try {
    const parsed = JSON.parse(s) as { column: string; op: FilterOp; values: string[] }[];
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((f) => f && typeof f.column === "string" && f.op && Array.isArray(f.values));
  } catch {
    return [];
  }
}

export function FilterBar({ cols, filters, onChange }: {
  cols: ColumnDef[];
  filters: ActiveFilter[];
  onChange: (fs: ActiveFilter[]) => void;
}) {
  const t = useT();
  const [draft, setDraft] = useState<{ column: string; op: FilterOp } | null>(null);
  const [draftVals, setDraftVals] = useState<string[]>([]);
  const filterable = cols.filter((c) => c.visible && c.fieldType !== "m2m" && !c.isComputed && opsFor(c).length > 0);
  const colByName = (n: string) => cols.find((c) => c.name === n);

  const add = () => { setDraft({ column: filterable[0].name, op: opsFor(filterable[0])[0] }); setDraftVals([""]); };
  const commit = () => {
    if (!draft) return;
    const vals = draft.op === "in"
      ? draftVals.join(",").split(",").map((v) => v.trim()).filter(Boolean)
      : draftVals;
    if (vals.length === 0 || vals.some((v) => v === "")) return;
    onChange([...filters.filter((f) => f.column !== draft.column), { column: draft.column, op: draft.op, values: vals }]);
    setDraft(null); setDraftVals([""]);
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      {filters.map((f) => (
        <span key={f.column} className="inline-flex items-center gap-1 rounded-full border border-blue-500/20 bg-blue-500/10 px-2 py-0.5 text-xs font-mono text-blue-600">
          {(colByName(f.column)?.label ?? f.column)} {opLabel(t, f.op)} {f.values.join(" … ")}
          <button onClick={() => onChange(filters.filter((x) => x.column !== f.column))} className="text-blue-400 hover:text-blue-600">
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
      {draft && (
        <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs">
          <Select value={draft.column} onValueChange={(v) => { setDraft({ column: v, op: opsFor(colByName(v)!)[0] }); setDraftVals([""]); }}>
            <SelectTrigger className="h-6 w-32 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>{filterable.map((c) => <SelectItem key={c.name} value={c.name}>{c.label}</SelectItem>)}</SelectContent>
          </Select>
          <Select value={draft.op} onValueChange={(v) => { setDraft({ ...draft, op: v as FilterOp }); setDraftVals(needCount(v as FilterOp) === 2 ? ["", ""] : [""]); }}>
            <SelectTrigger className="h-6 w-24 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>{opsFor(colByName(draft.column)!).map((o) => <SelectItem key={o} value={o}>{opLabel(t, o)}</SelectItem>)}</SelectContent>
          </Select>
          {renderValueInput(t, colByName(draft.column)!, draft.op, draftVals, setDraftVals)}
          <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={commit}>{t("filter.ok")}</Button>
          <button onClick={() => setDraft(null)} className="text-muted-foreground"><X className="h-3 w-3" /></button>
        </span>
      )}
      <Button size="sm" variant="outline" onClick={add} disabled={filterable.length === 0}>
        <Filter className="h-3.5 w-3.5" /> {t("filter.add")}
      </Button>
    </div>
  );
}

function renderValueInput(t: (key: string) => string, c: ColumnDef, op: FilterOp, vals: string[], set: (v: string[]) => void) {
  const n = needCount(op);
  if (c.fieldType === "boolean") {
    return (
      <Select value={vals[0]} onValueChange={(v) => set([v])}>
        <SelectTrigger className="h-6 w-24 text-xs"><SelectValue placeholder={t("filter.select")} /></SelectTrigger>
        <SelectContent>
          <SelectItem value="true">true</SelectItem>
          <SelectItem value="false">false</SelectItem>
        </SelectContent>
      </Select>
    );
  }
  if (c.fieldType === "enum" && c.enumOptions) {
    return (
      <Select value={vals[0]} onValueChange={(v) => set([v])}>
        <SelectTrigger className="h-6 w-32 text-xs"><SelectValue placeholder={t("filter.select")} /></SelectTrigger>
        <SelectContent>{c.enumOptions.map((o) => <SelectItem key={o} value={o}>{o}</SelectItem>)}</SelectContent>
      </Select>
    );
  }
  const type = c.fieldType === "number" ? "number" : c.fieldType === "datetime" ? "datetime-local" : "text";
  if (n === 2) {
    return (
      <span className="flex items-center gap-1">
        <Input type={type} className="h-6 w-40 text-xs" value={vals[0] ?? ""} onChange={(e) => set([e.target.value, vals[1] ?? ""])} />
        <Input type={type} className="h-6 w-40 text-xs" value={vals[1] ?? ""} onChange={(e) => set([vals[0] ?? "", e.target.value])} />
      </span>
    );
  }
  if (n === -1) {
    return <Input className="h-6 w-52 text-xs" placeholder="a,b,c" value={vals[0] ?? ""} onChange={(e) => set([e.target.value])} />;
  }
  return <Input type={type} className="h-6 w-40 text-xs" value={vals[0] ?? ""} onChange={(e) => set([e.target.value])} />;
}
