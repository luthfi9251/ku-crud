import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronUp,
  Settings2,
  Key,
  Link2,
  ListFilter,
  Check,
  HelpCircle,
  Info,
  Layers,
  Plus,
  Trash2,
} from "lucide-react";
import { api } from "../lib/api";
import type { BaseFieldType, ColumnDef, ColumnFormatting, Datasource, TableDefPayload, ValidationRuleType } from "../lib/types";
import { useT } from "../lib/i18n";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const fieldTypes = ["boolean", "text", "number", "datetime", "enum", "uuid", "json", "fk"] as const;
const computedFieldTypes = ["number", "text"] as const;

export interface FormCol extends ColumnDef {
  livePk?: boolean;
  origType?: BaseFieldType;
  fkDs?: string;
}

interface ColumnListEditorProps {
  cols: FormCol[];
  keys: string[];
  toggleKey: (name: string) => void;
  setCol: (index: number, patch: Partial<FormCol>) => void;
  dsId: string;
  currentId?: string;
  defs: TableDefPayload[];
  dsList: Datasource[];
  isLoadingCols?: boolean;
  onAddComputed?: () => void;
}

export function HelpPopover({
  title,
  children,
  placement = "top",
}: {
  title: string;
  children: React.ReactNode;
  placement?: "top" | "bottom" | "bottom-start";
}) {
  const [open, setOpen] = useState(false);
  const t = useT();

  const getPositionClasses = () => {
    switch (placement) {
      case "bottom":
        return "top-full left-1/2 -translate-x-1/2 mt-2";
      case "bottom-start":
        return "top-full left-0 mt-2";
      case "top":
      default:
        return "bottom-full left-1/2 -translate-x-1/2 mb-2";
    }
  };

  return (
    <div className="relative inline-flex items-center">
      <button
        type="button"
        className="text-muted-foreground/70 hover:text-blue-500 transition-colors p-0.5 rounded-full outline-none"
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onClick={(e) => {
          e.preventDefault();
          setOpen((prev) => !prev);
        }}
        aria-label={t("col.helpAria")}
        title={title}
      >
        <HelpCircle className="h-3.5 w-3.5" />
      </button>

      {open && (
        <div
          className={`absolute w-72 p-3 rounded-lg border bg-popover text-popover-foreground text-xs shadow-xl z-50 animate-in fade-in-0 zoom-in-95 pointer-events-none ${getPositionClasses()}`}
        >
          <div className="font-semibold text-xs text-foreground mb-1.5 flex items-center gap-1.5 border-b pb-1 border-border/40">
            <Info className="h-3.5 w-3.5 text-blue-500 shrink-0" />
            <span>{title}</span>
          </div>
          <div className="text-[11px] leading-relaxed text-muted-foreground space-y-1 font-normal">
            {children}
          </div>
          {/* Arrow */}
          <div
            className={`absolute border-4 border-transparent ${
              placement.startsWith("bottom")
                ? "bottom-full left-3 -mb-1 border-b-popover"
                : "top-full left-1/2 -translate-x-1/2 -mt-1 border-t-popover"
            }`}
          />
        </div>
      )}
    </div>
  );
}

export function ColumnListEditor({
  cols,
  keys,
  toggleKey,
  setCol,
  dsId,
  currentId,
  defs,
  dsList,
  isLoadingCols = false,
  onAddComputed,
}: ColumnListEditorProps) {
  // Store expanded state per column name
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const t = useT();

  const toggleExpand = (colName: string) => {
    setExpanded((prev) => ({ ...prev, [colName]: !prev[colName] }));
  };

  const setTypeAndExpand = (i: number, colName: string, newType: ColumnDef["fieldType"], origCol: FormCol) => {
    setCol(i, {
      fieldType: newType,
      enumOptions: newType === "enum" ? (origCol.enumOptions ?? []) : null,
      ...(newType === "fk"
        ? { baseType: origCol.origType ?? (origCol.fieldType === "fk" ? origCol.baseType : "text") }
        : { baseType: undefined, fkTableDefId: undefined, fkRefColumn: undefined, fkDisplayColumns: undefined }),
    });

    if (newType === "enum" || newType === "fk") {
      setExpanded((prev) => ({ ...prev, [colName]: true }));
    }
  };

  const handleColPatch = (i: number, patch: Partial<FormCol>) => {
    setCol(i, patch);
  };

  if (isLoadingCols) {
    return (
      <div className="flex h-32 items-center justify-center rounded-lg border bg-card text-xs text-muted-foreground">
        {t("col.inspecting")}
      </div>
    );
  }

  if (cols.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center rounded-lg border bg-card text-xs text-muted-foreground">
        {t("col.noMappings")}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Step 3 Header Help & Counter */}
      <div className="flex items-center justify-between px-1 text-xs text-muted-foreground font-medium">
        <div className="flex items-center gap-1.5">
          <span>{t("col.listTitle", { count: String(cols.length) })}</span>
          <HelpPopover title={t("col.guideTitle")}>
            <p>
              {t("col.guide1")}
            </p>
            <p className="pt-1">
              {t("col.guide2")}
            </p>
          </HelpPopover>
        </div>
        <div className="flex items-center gap-2">
          <span>{t("col.clickSettings")}</span>
          {onAddComputed && (
            <Button type="button" variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={onAddComputed}>
              <Plus className="h-3.5 w-3.5" /> {t("col.addComputed")}
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-3">
        {cols.map((c, i) => {
          const isKey = keys.includes(c.name);
          const isExpanded = !!expanded[c.name] || c.fieldType === "enum" || c.fieldType === "fk";
          const hasCustomConfig = c.fieldType === "enum" || c.fieldType === "fk";

          return (
            <div
              key={c.name}
              className={`rounded-xl border transition-all duration-200 shadow-xs bg-card ${
                isExpanded ? "border-blue-500/40 ring-1 ring-blue-500/10" : "border-border/70 hover:border-border"
              }`}
            >
              {/* Main Column Header Row */}
              <div className="p-3.5 space-y-3">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                  {/* Column Identity & Key Checkbox */}
                  <div className="flex items-center gap-2 min-w-[230px]">
                    <input
                      type="checkbox"
                      id={`key-${c.name}`}
                      className="h-4 w-4 accent-amber-500 rounded cursor-pointer shrink-0"
                      checked={isKey}
                       onChange={() => toggleKey(c.name)}
                       title={t("col.useAsKey")}
                     />
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <label htmlFor={`key-${c.name}`} className="font-mono text-xs font-bold tracking-tight cursor-pointer">
                        {c.name}
                      </label>
                      {isKey && (
                        <Badge variant="outline" className="text-[10px] bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20 font-mono gap-0.5">
                          <Key className="h-2.5 w-2.5" /> PK
                        </Badge>
                      )}
                      {c.required && (
                        <Badge variant="outline" className="text-[9px] bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20 font-mono">
                          NOT NULL
                        </Badge>
                      )}
                      {/* Tooltip Composite Key Logic */}
                       <HelpPopover title={t("col.keyTitle")}>
                         <p>
                           <strong>{t("col.keyTerm")}</strong> {t("col.keyDesc")}
                         </p>
                         <p className="pt-1">
                           <strong>{t("col.keyBackendLabel")}</strong> {t("col.keyBackendDesc")}
                         </p>
                         <p className="pt-1 text-[10px] italic">
                           {t("col.keyGuide3")}
                         </p>
                       </HelpPopover>
                    </div>
                  </div>

                  {/* Display Label & Field Type Select */}
                  <div className="flex flex-wrap items-center gap-2 flex-1">
                    {/* Display Label Input */}
                    <div className="space-y-1 flex-1 min-w-[150px]">
                      <div className="flex items-center gap-1">
                        <Label className="text-[10px] text-muted-foreground font-medium">{t("col.displayLabel")}</Label>
                        <HelpPopover title={t("col.displayLabel")}>
                          {t("col.displayLabelHelp")}
                        </HelpPopover>
                      </div>
                      <Input
                        value={c.label}
                        onChange={(e) => setCol(i, { label: e.target.value })}
                        className="h-8 text-xs bg-muted/20"
                        placeholder={t("col.labelPh")}
                      />
                    </div>

                    {/* Field Type Select */}
                    <div className="space-y-1 w-36">
                      <div className="flex items-center gap-1">
                        <Label className="text-[10px] text-muted-foreground font-medium">{t("col.fieldType")}</Label>
                        <HelpPopover title={t("col.fieldTypeTitle")}>
                          <p className="mb-1">{t("col.fieldTypeIntro")}</p>
                          <ul className="space-y-1 list-disc pl-3 text-[10px]">
                            <li><strong>boolean</strong>: {t("col.ftBoolDesc")}</li>
                            <li><strong>text</strong>: {t("col.ftTextDesc")}</li>
                            <li><strong>number</strong>: {t("col.ftNumberDesc")}</li>
                            <li><strong>datetime</strong>: {t("col.ftDatetimeDesc")}</li>
                            <li><strong>enum</strong>: {t("col.ftEnumDesc")}</li>
                            <li><strong>fk</strong>: {t("col.ftFkDesc")}</li>
                          </ul>
                        </HelpPopover>
                      </div>
                      <Select
                        value={c.fieldType}
                        onValueChange={(v) => setTypeAndExpand(i, c.name, v as ColumnDef["fieldType"], c)}
                      >
                        <SelectTrigger className="h-8 text-xs font-medium">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {fieldTypes.map((t) => (
                            <SelectItem key={t} value={t} className="text-xs">
                              <span className="font-mono">{t}</span>
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  </div>

                  {/* Property Toggles & Expand Button */}
                  <div className="flex items-center justify-between lg:justify-end gap-3 pt-2 lg:pt-0 border-t lg:border-t-0 border-border/40">
                    <div className="flex items-center gap-1">
                      <div className="grid grid-cols-5 gap-2 text-center">
                        <PropertyToggle label={t("col.swEdit")} checked={c.editable} onChange={(v) => handleColPatch(i, { editable: v })} />
                        <PropertyToggle label={t("col.swReq")} checked={c.required} onChange={(v) => handleColPatch(i, { required: v })} />
                        <PropertyToggle label={t("col.swVis")} checked={c.visible} onChange={(v) => handleColPatch(i, { visible: v })} />
                        <PropertyToggle label={t("col.swSrch")} checked={c.searchable} onChange={(v) => handleColPatch(i, { searchable: v })} />
                        <PropertyToggle label={t("col.swSort")} checked={c.sortable} onChange={(v) => handleColPatch(i, { sortable: v })} />
                      </div>
                      {/* Property Switch Tooltip */}
                      <HelpPopover title={t("col.switchesTitle")}>
                        <ul className="space-y-1.5 text-[10px]">
                          <li><strong>{t("col.swEditLabel")}</strong>: {t("col.swEditDesc")}</li>
                          <li><strong>{t("col.swReqLabel")}</strong>: {t("col.swReqDesc")}</li>
                          <li><strong>{t("col.swVisLabel")}</strong>: {t("col.swVisDesc")}</li>
                          <li><strong>{t("col.swSrchLabel")}</strong>: {t("col.swSrchDesc")}</li>
                          <li><strong>{t("col.swSortLabel")}</strong>: {t("col.swSortDesc")}</li>
                        </ul>
                        <div className="pt-1.5 mt-1 border-t border-border/40 text-[10px] text-muted-foreground">
                          💡 <strong>{t("col.pkPolicyLabel")}</strong> {t("col.pkPolicy")}
                        </div>
                      </HelpPopover>
                    </div>

                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className={`h-8 px-2 text-xs gap-1 shrink-0 ${hasCustomConfig ? "text-blue-600 dark:text-blue-400" : "text-muted-foreground"}`}
                      onClick={() => toggleExpand(c.name)}
                      title={t("col.toggleConfig")}
                    >
                      <Settings2 className="h-3.5 w-3.5" />
                      {hasCustomConfig && (
                        <Badge variant="outline" className="text-[9px] px-1 py-0 bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20 font-mono">
                          {c.fieldType.toUpperCase()}
                        </Badge>
                      )}
                      {isExpanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                    </Button>
                  </div>
                </div>
              </div>

              {/* Inline Expandable Configuration Panel */}
              {isExpanded && (
                <div className="border-t bg-muted/20 p-4 space-y-4 rounded-b-xl">
                  {c.fieldType === "enum" && (
                    <EnumConfigPanel col={c} index={i} setCol={setCol} />
                  )}

                  {c.fieldType === "fk" && (
                    <FKConfigInline
                      col={c}
                      index={i}
                      dsId={dsId}
                      currentId={currentId}
                      defs={defs}
                      dsList={dsList}
                      cols={cols}
                      setCol={setCol}
                    />
                  )}

                  {c.isComputed && (
                    <ComputedEditor col={c} index={i} setCol={setCol} />
                  )}

                  {(c.fieldType === "enum" || c.fieldType === "number") && !c.isComputed && (
                    <FormattingEditor col={c} index={i} setCol={setCol} />
                  )}

                  {c.editable && c.fieldType !== "m2m" && c.fieldType !== "fk" && (
                    <ValidationsEditor col={c} index={i} setCol={setCol} />
                  )}

                  {c.fieldType !== "enum" && c.fieldType !== "fk" && (
                    <div className="text-xs text-muted-foreground flex items-center gap-2 italic">
                      <span>{t("col.standardProps1")} <strong className="font-mono">{c.fieldType}</strong> {t("col.standardProps2")}</span>
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PropertyToggle({
  label,
  checked,
  disabled = false,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  const t = useT();
  return (
    <div className="flex flex-col items-center gap-1" title={disabled ? t("col.disabled") : undefined}>
      <span className="text-[9px] text-muted-foreground font-mono uppercase tracking-wider">{label}</span>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} className="scale-75 origin-center" />
    </div>
  );
}

function ValidationsEditor({
  col,
  index,
  setCol,
}: {
  col: FormCol;
  index: number;
  setCol: (i: number, patch: Partial<FormCol>) => void;
}) {
  const t = useT();
  const rules = col.validations ?? [];
  const has = (t: ValidationRuleType) => rules.some((r) => r.type === t);
  const param = (t: ValidationRuleType) => rules.find((r) => r.type === t)?.param ?? 0;
  const set = (t: ValidationRuleType, on: boolean, p?: number) => {
    let next = rules.filter((r) => r.type !== t);
    if (on) next = [...next, { type: t, ...(p != null ? { param: p } : {}) }];
    setCol(index, { validations: next.length ? next : null });
  };
  return (
    <div className="flex flex-wrap items-center gap-3 pt-1">
      <span className="text-[11px] font-medium text-muted-foreground">{t("col.validations")}</span>
      {(["email", "number", "text"] as const).map((t2) => (
        <label key={t2} className="flex items-center gap-1 text-xs">
          <Checkbox checked={has(t2)} onChange={(e) => set(t2, e.target.checked)} />
          {t2 === "email" ? t("col.vEmail") : t2 === "number" ? t("col.vNumber") : t("col.vText")}
        </label>
      ))}
      {(["min_len", "max_len"] as const).map((t2) => (
        <label key={t2} className="flex items-center gap-1 text-xs">
          <Checkbox checked={has(t2)} onChange={(e) => set(t2, e.target.checked, param(t2) || 1)} />
          {t2 === "min_len" ? t("col.vMin") : t("col.vMax")} {t("col.vLen")}
          <Input type="number" className="h-6 w-16" min={1} max={1000} disabled={!has(t2)}
            value={has(t2) ? param(t2) : ""} onChange={(e) => set(t2, true, Number(e.target.value) || 1)} />
        </label>
      ))}
    </div>
  );
}

function FormattingEditor({ col, index, setCol }: {
  col: FormCol; index: number; setCol: (i: number, patch: Partial<FormCol>) => void;
}) {
  const t = useT();
  const fmt = col.formatting ?? {};
  const setFmt = (next: ColumnFormatting) => setCol(index, { formatting: next });

  if (col.fieldType === "enum") {
    const colors = fmt.enumColors ?? {};
    const PICK = ["gray", "blue", "green", "amber", "red", "purple", "cyan", "orange"];
    return (
      <div className="space-y-2 rounded-lg border border-sky-500/20 bg-sky-500/5 p-3.5">
        <Label className="text-xs font-semibold text-sky-700 dark:text-sky-300">{t("col.enumColors")}</Label>
        <div className="flex flex-wrap gap-2">
          {(col.enumOptions ?? []).map((o) => (
            <div key={o} className="flex items-center gap-1 rounded-md border border-border bg-background px-2 py-1">
              <span className="text-[10px] font-mono">{o}</span>
              <Select value={colors[o] ?? "gray"} onValueChange={(v) => setFmt({ ...fmt, enumColors: { ...colors, [o]: v } })}>
                <SelectTrigger className="h-6 w-20 text-[10px]"><SelectValue /></SelectTrigger>
                <SelectContent>{PICK.map((p) => <SelectItem key={p} value={p} className="text-[10px]">{p}</SelectItem>)}</SelectContent>
              </Select>
            </div>
          ))}
        </div>
      </div>
    );
  }

  const nf = fmt.number ?? {};
  return (
    <div className="space-y-2 rounded-lg border border-sky-500/20 bg-sky-500/5 p-3.5">
      <Label className="text-xs font-semibold text-sky-700 dark:text-sky-300">{t("col.numberFmt")}</Label>
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-1 text-xs">
          <Checkbox checked={!!nf.thousands}
            onChange={(e) => setFmt({ ...fmt, number: { ...nf, thousands: e.target.checked } })} />
          {t("col.thousands")}
        </label>
        <label className="flex items-center gap-1 text-xs">
          {t("col.decimals")}
          <Input type="number" min={0} max={6} className="h-7 w-14 text-xs"
            value={nf.decimals ?? 0}
            onChange={(e) => setFmt({ ...fmt, number: { ...nf, decimals: Number(e.target.value) || 0 } })} />
        </label>
        <label className="flex items-center gap-1 text-xs">
          {t("col.prefix")}
          <Input className="h-7 w-24 text-xs" value={nf.prefix ?? ""} placeholder="Rp "
            onChange={(e) => setFmt({ ...fmt, number: { ...nf, prefix: e.target.value } })} />
        </label>
      </div>
    </div>
  );
}

function ComputedEditor({ col, index, setCol }: {
  col: FormCol; index: number; setCol: (i: number, patch: Partial<FormCol>) => void;
}) {
  const t = useT();
  return (
    <div className="space-y-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3.5">
      <div className="flex items-center gap-2">
        <span className="text-[11px] font-medium text-emerald-700 dark:text-emerald-300">{t("col.computed")}</span>
        <HelpPopover title={t("col.computedTitle")} placement="bottom">
          <p>{t("col.computed1")}</p>
          <p className="pt-1 text-[10px] font-mono">price * qty + 5 · CONCAT(first, " ", last)</p>
          <p className="pt-1">{t("col.computed2")}</p>
        </HelpPopover>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <div className="w-28">
          <Label className="text-[10px] text-muted-foreground">{t("col.resultType")}</Label>
          <Select value={col.fieldType} onValueChange={(v) => setCol(index, { fieldType: v as FormCol["fieldType"] })}>
            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {(computedFieldTypes as readonly string[]).map((t) => (
                <SelectItem key={t} value={t} className="text-xs font-mono">{t}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex-1 min-w-[260px]">
          <Label className="text-[10px] text-muted-foreground">{t("col.formula")}</Label>
          <Input className="h-8 text-xs font-mono" value={col.computedFormula ?? ""}
            placeholder={'e.g. price * qty + 5  or  CONCAT(first, " ", last)'}
            onChange={(e) => setCol(index, { computedFormula: e.target.value })} />
        </div>
      </div>
      <p className="text-[10px] text-muted-foreground">
        {t("col.computed3")}
      </p>
    </div>
  );
}

function EnumConfigPanel({
  col,
  index,
  setCol,
}: {
  col: FormCol;
  index: number;
  setCol: (i: number, patch: Partial<FormCol>) => void;
}) {
  const t = useT();
  const options = col.enumOptions ?? [];
  const rawInput = options.join(", ");

  return (
    <div className="space-y-2 rounded-lg border border-purple-500/20 bg-purple-500/5 p-3.5">
      <div className="flex items-center gap-2">
        <ListFilter className="h-4 w-4 text-purple-600 dark:text-purple-400" />
        <Label className="text-xs font-semibold text-purple-900 dark:text-purple-300">
          {t("col.enumConfig", { name: col.name })}
        </Label>
        <HelpPopover title={t("col.enumGuide")}>
          {t("col.enumGuideBody")}
        </HelpPopover>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {t("col.enumHint")}
      </p>
      <Input
        className="h-8 text-xs font-mono bg-background"
        placeholder="e.g. ACTIVE, INACTIVE, PENDING"
        value={rawInput}
        onChange={(e) =>
          setCol(index, {
            enumOptions: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
          })
        }
      />
      {options.length > 0 && (
        <div className="flex items-center gap-1.5 flex-wrap pt-1">
          <span className="text-[10px] text-muted-foreground font-medium">{t("col.parsedOptions")}</span>
          {options.map((o) => (
            <Badge key={o} variant="outline" className="text-[10px] font-mono bg-purple-500/10 text-purple-700 dark:text-purple-300 border-purple-500/20">
              {o}
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}

function FKConfigInline({
  col,
  index,
  dsId,
  currentId,
  defs,
  dsList,
  cols,
  setCol,
}: {
  col: FormCol;
  index: number;
  dsId: string;
  currentId?: string;
  defs: TableDefPayload[];
  dsList: Datasource[];
  cols: FormCol[];
  setCol: (i: number, patch: Partial<FormCol>) => void;
}) {
  const t = useT();
  const fkDs = col.fkDs ?? dsId;
  const targetDefs = defs.filter((d) => d.datasourceId === fkDs);
  const isSelf = col.fkTableDefId === "self" || (!!currentId && col.fkTableDefId === currentId);

  const targetDefQ = useQuery({
    queryKey: ["def", col.fkTableDefId],
    enabled: !!col.fkTableDefId && !isSelf,
    queryFn: () => api<TableDefPayload>(`/tables/${col.fkTableDefId}`),
  });

  const targetCols = isSelf ? cols : (targetDefQ.data?.columns ?? []);
  const availableDisplayCols = targetCols.filter((tc) => tc.name !== col.fkRefColumn);
  const selectedDisplayCols = col.fkDisplayColumns ?? [];

  const handleSelectAll = () => {
    setCol(index, {
      fkDisplayColumns: availableDisplayCols.map((c) => c.name),
    });
  };

  const handleClearAll = () => {
    setCol(index, {
      fkDisplayColumns: [],
    });
  };

  return (
    <div className="space-y-4 rounded-lg border border-blue-500/20 bg-blue-500/5 p-4">
      {/* FK Header */}
      <div className="flex items-center justify-between pb-1 border-b border-blue-500/10">
        <div className="flex items-center gap-2">
          <Link2 className="h-4 w-4 text-blue-600 dark:text-blue-400" />
          <Label className="text-xs font-semibold text-blue-900 dark:text-blue-300">
            {t("col.fkConfig", { name: col.name })}
          </Label>
          <HelpPopover title={t("col.fkGuide")}>
            <p>
              {t("col.fkGuide1")}
            </p>
            <p className="pt-1">
              {t("col.fkGuide2")}
            </p>
          </HelpPopover>
        </div>
        <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
          {t("col.targetRef", { ref: col.fkRefColumn || "—" })}
        </Badge>
      </div>

      {/* Primary Target Selectors (3 Columns) */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {/* Target Datasource */}
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">{t("col.fkTargetDs")}</Label>
          <Select
            value={fkDs}
            onValueChange={(v) =>
              setCol(index, { fkDs: v, fkTableDefId: undefined, fkRefColumn: undefined, fkDisplayColumns: undefined })
            }
          >
            <SelectTrigger className="h-9 text-xs bg-background">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {dsList.map((d) => (
                <SelectItem key={d.id} value={String(d.id)} className="text-xs">
                  {d.name} ({d.dbname})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Related Table */}
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">{t("col.fkRelatedTable")}</Label>
          <Select
            value={col.fkTableDefId ?? ""}
            onValueChange={(v) =>
              setCol(index, { fkTableDefId: v, fkRefColumn: undefined, fkDisplayColumns: undefined })
            }
          >
            <SelectTrigger className="h-9 text-xs bg-background">
              <SelectValue placeholder={t("col.fkChooseTable")} />
            </SelectTrigger>
            <SelectContent>
              {fkDs === dsId && (
                <SelectItem value="self" className="text-xs">
                  {t("col.fkSelf")}
                </SelectItem>
              )}
              {targetDefs
                .filter((d) => String(d.id) !== currentId)
                .map((d) => (
                  <SelectItem key={d.id} value={String(d.id)} className="text-xs">
                    {d.label} ({d.tableName})
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </div>

        {/* Reference Column */}
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">{t("col.fkRefColumn")}</Label>
          <Select
            value={col.fkRefColumn ?? ""}
            onValueChange={(v) => setCol(index, { fkRefColumn: v, fkDisplayColumns: undefined })}
            disabled={!col.fkTableDefId}
          >
            <SelectTrigger className="h-9 text-xs bg-background font-mono">
              <SelectValue placeholder={t("col.fkSelectRef")} />
            </SelectTrigger>
            <SelectContent>
              {targetCols.map((tc) => (
                <SelectItem key={tc.name} value={tc.name} className="text-xs font-mono">
                  {tc.name} ({tc.fieldType})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Full Width Display Columns Selector */}
      <div className="space-y-2 pt-2 border-t border-blue-500/10">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
           <div className="flex items-center gap-2">
             <Label className="text-xs font-medium text-foreground">
               {t("col.fkDisplayCols")}
             </Label>
             <Badge variant="secondary" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
               {t("col.nOfMSelected", { selected: String(selectedDisplayCols.length), total: String(availableDisplayCols.length) })}
             </Badge>
           </div>

           {col.fkTableDefId && availableDisplayCols.length > 0 && (
             <div className="flex items-center gap-2">
               <Button
                 type="button"
                 variant="outline"
                 size="sm"
                 className="h-7 text-[11px] px-2 bg-background hover:bg-muted"
                 onClick={handleSelectAll}
               >
                 {t("col.selectAll")}
               </Button>
               <Button
                 type="button"
                 variant="ghost"
                 size="sm"
                 className="h-7 text-[11px] px-2 text-muted-foreground hover:text-destructive"
                 onClick={handleClearAll}
               >
                 {t("col.clearAll")}
               </Button>
             </div>
           )}
         </div>

         {!col.fkTableDefId ? (
           <div className="p-3 text-center text-xs text-muted-foreground italic rounded-md border border-dashed bg-background/50">
             {t("col.fkPickTableFirst")}
           </div>
         ) : availableDisplayCols.length === 0 ? (
           <div className="p-3 text-center text-xs text-muted-foreground italic rounded-md border border-dashed bg-background/50">
             {t("col.fkNoMoreCols")}
           </div>
         ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-2 pt-1">
            {availableDisplayCols.map((tc) => {
              const sel = selectedDisplayCols.includes(tc.name);
              return (
                <button
                  type="button"
                  key={tc.name}
                  onClick={() =>
                    setCol(index, {
                      fkDisplayColumns: sel
                        ? selectedDisplayCols.filter((n) => n !== tc.name)
                        : [...selectedDisplayCols, tc.name],
                    })
                  }
                  className={`flex items-center justify-between gap-1.5 px-3 py-2 rounded-lg border text-xs font-mono transition-all text-left ${
                    sel
                      ? "bg-blue-500/15 border-blue-500/70 text-blue-800 dark:text-blue-200 font-semibold shadow-xs ring-1 ring-blue-500/20"
                      : "bg-background border-border/80 text-muted-foreground hover:border-blue-500/40 hover:bg-muted/40"
                  }`}
                >
                  <span className="truncate">{tc.name}</span>
                  {sel ? (
                    <Check className="h-3.5 w-3.5 text-blue-600 dark:text-blue-400 shrink-0" />
                  ) : (
                    <span className="h-3.5 w-3.5 rounded-full border border-muted-foreground/30 shrink-0" />
                  )}
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Many-to-Many relations (v1.3): virtual columns backed by a junction table
// that must already be defined with two fk columns.
// ---------------------------------------------------------------------------

export function M2MRelationsEditor({
  cols, setCols, defs, currentId,
}: {
  cols: FormCol[];
  setCols: React.Dispatch<React.SetStateAction<FormCol[]>>;
  defs: TableDefPayload[];
  currentId?: string;
}) {
  const t = useT();
  const relations = cols.filter((c) => c.fieldType === "m2m");
  const candidates = defs.filter((d) => String(d.id) !== currentId);

  const setRel = (i: number, patch: Partial<FormCol>) => {
    const idx = cols.findIndex((c) => c.fieldType === "m2m" && c.name === relations[i].name);
    if (idx >= 0) setCols((prev) => prev.map((c, j) => (j === idx ? { ...c, ...patch } : c)));
  };
  const removeRel = (i: number) => {
    setCols((prev) => prev.filter((c) => !(c.fieldType === "m2m" && c.name === relations[i].name)));
  };
  const addRel = () => {
    setCols((prev) => [
      ...prev,
      {
        name: `m2m_${prev.length}`,
        label: t("col.newRelation"),
        fieldType: "m2m",
        enumOptions: null,
        editable: true, required: false, visible: true, searchable: false, sortable: false,
        position: 1000 + prev.length,
        m2mJunctionDefId: undefined,
        m2mJunctionSrcCol: undefined,
        m2mJunctionTgtCol: undefined,
        m2mDisplayColumns: [],
      },
    ]);
  };

  return (
    <div className="space-y-3 rounded-lg border border-violet-500/20 bg-violet-500/5 p-4">
      <div className="flex items-center justify-between border-b border-violet-500/10 pb-2">
        <div className="flex items-center gap-2">
          <Layers className="h-4 w-4 text-violet-600 dark:text-violet-400" />
          <Label className="text-xs font-semibold text-violet-900 dark:text-violet-300">
            {t("col.m2mTitle")}
          </Label>
          <HelpPopover title={t("col.m2mGuide")}>
            <p>{t("col.m2mGuide1")}</p>
            <p className="pt-1">{t("col.m2mGuide2")}</p>
            <p className="pt-1 text-[10px]">💡 {t("col.m2mGuide3")}</p>
          </HelpPopover>
        </div>
        <Button type="button" variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={addRel} disabled={!currentId}>
          <Plus className="h-3.5 w-3.5" /> {t("col.addRelation")}
        </Button>
      </div>
      {!currentId && (
        <p className="text-[11px] text-muted-foreground">
          {t("col.m2mSaveFirst")}
        </p>
      )}
      {relations.map((rel, i) => (
        <M2MRelationCard
          key={rel.name}
          rel={rel}
          setRel={(patch) => setRel(i, patch)}
          remove={() => removeRel(i)}
          candidates={candidates}
          currentId={currentId}
        />
      ))}
    </div>
  );
}

function M2MRelationCard({
  rel, setRel, remove, candidates, currentId,
}: {
  rel: FormCol;
  setRel: (patch: Partial<FormCol>) => void;
  remove: () => void;
  candidates: TableDefPayload[];
  currentId?: string;
}) {
  const t = useT();
  const junctionQ = useQuery({
    queryKey: ["def", rel.m2mJunctionDefId],
    enabled: !!rel.m2mJunctionDefId,
    queryFn: () => api<TableDefPayload>(`/tables/${rel.m2mJunctionDefId}`),
  });
  const jcols = junctionQ.data?.columns ?? [];
  const fkCols = jcols.filter((c) => c.fieldType === "fk");
  const srcOptions = fkCols.filter((c) => c.fkTableDefId === currentId);
  const tgtOptions = fkCols.filter((c) => c.name !== rel.m2mJunctionSrcCol);

  const tgtCol = fkCols.find((c) => c.name === rel.m2mJunctionTgtCol);
  const targetQ = useQuery({
    queryKey: ["def", tgtCol?.fkTableDefId],
    enabled: !!tgtCol?.fkTableDefId,
    queryFn: () => api<TableDefPayload>(`/tables/${tgtCol!.fkTableDefId}`),
  });
  const targetCols = targetQ.data?.columns ?? [];
  const selectedDisplay = rel.m2mDisplayColumns ?? [];

  const junctionTable = candidates.find((d) => String(d.id) === rel.m2mJunctionDefId);

  return (
    <div className="space-y-3 rounded-md border bg-background p-3">
      <div className="flex items-center gap-2">
        <div className="flex-1 flex items-center gap-1.5">
           <Input
             className="h-8 flex-1 text-xs"
             value={rel.label}
             onChange={(e) => setRel({ label: e.target.value })}
             placeholder={t("col.relLabelPh")}
           />
           <HelpPopover title={t("col.relLabelTitle")} placement="bottom-start">
             <p>{t("col.relLabelDesc")}</p>
           </HelpPopover>
         </div>
         <Button type="button" variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-destructive" onClick={remove} title={t("col.removeRelation")}>
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="space-y-1">
           <div className="flex items-center gap-1">
             <Label className="text-[11px] text-muted-foreground font-medium">{t("col.junctionTable")}</Label>
             <HelpPopover title={t("col.junctionTable")} placement="bottom">
               <p>{t("col.junctionHelp")}</p>
             </HelpPopover>
           </div>
          <Select
            value={rel.m2mJunctionDefId ?? ""}
            onValueChange={(v) =>
              setRel({
                m2mJunctionDefId: v,
                m2mJunctionSrcCol: undefined,
                m2mJunctionTgtCol: undefined,
                m2mDisplayColumns: [],
                name: `m2m_${defsTableName(v, candidates)}_${rel.name}`,
              })
            }
          >
             <SelectTrigger className="h-8 text-xs bg-background">
               <SelectValue placeholder={t("col.chooseJunction")} />
             </SelectTrigger>
            <SelectContent>
              {candidates.map((d) => (
                <SelectItem key={d.id} value={String(d.id)} className="text-xs">
                  {d.label} ({d.tableName})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
           <div className="flex items-center gap-1">
             <Label className="text-[11px] text-muted-foreground font-medium">{t("col.linkVia")}</Label>
             <HelpPopover title={t("col.srcFkTitle")} placement="bottom">
               <p>{t("col.srcFkHelp")}</p>
             </HelpPopover>
           </div>
          <Select
            value={rel.m2mJunctionSrcCol ?? ""}
            onValueChange={(v) => setRel({ m2mJunctionSrcCol: v, m2mJunctionTgtCol: undefined, m2mDisplayColumns: [] })}
            disabled={!rel.m2mJunctionDefId}
          >
             <SelectTrigger className="h-8 text-xs bg-background">
               <SelectValue placeholder={t("col.junctionToThis")} />
             </SelectTrigger>
            <SelectContent>
              {srcOptions.map((c) => (
                <SelectItem key={c.name} value={c.name} className="text-xs">
                  {c.label} ({c.name})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
           {rel.m2mJunctionDefId && srcOptions.length === 0 && (
             <p className="text-[10px] text-amber-600">{t("col.noJunctionFk")}</p>
           )}
        </div>
        <div className="space-y-1">
           <div className="flex items-center gap-1">
             <Label className="text-[11px] text-muted-foreground font-medium">{t("col.relatedVia")}</Label>
             <HelpPopover title={t("col.tgtFkTitle")} placement="bottom">
               <p>{t("col.tgtFkHelp")}</p>
             </HelpPopover>
           </div>
          <Select
            value={rel.m2mJunctionTgtCol ?? ""}
            onValueChange={(v) => {
              const t = fkCols.find((c) => c.name === v);
              setRel({
                m2mJunctionTgtCol: v,
                m2mDisplayColumns: [],
                name: `m2m_${junctionTable?.tableName ?? "j"}_${v}`,
                label: t?.label ?? rel.label,
              });
            }}
            disabled={!rel.m2mJunctionSrcCol}
          >
             <SelectTrigger className="h-8 text-xs bg-background">
               <SelectValue placeholder={t("col.junctionToTarget")} />
             </SelectTrigger>
            <SelectContent>
              {tgtOptions.map((c) => (
                <SelectItem key={c.name} value={c.name} className="text-xs">
                  {c.label} ({c.name})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      {rel.m2mJunctionTgtCol && (
        <div className="space-y-1.5">
           <div className="flex items-center justify-between">
             <div className="flex items-center gap-1">
               <Label className="text-[11px] text-muted-foreground font-medium">{t("col.targetDisplayCols")}</Label>
               <HelpPopover title={t("col.targetDisplayTitle")} placement="bottom-start">
                 <p>{t("col.targetDisplayHelp")}</p>
               </HelpPopover>
             </div>
             <div className="flex gap-1">
               <Button type="button" variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => setRel({ m2mDisplayColumns: targetCols.map((c) => c.name) })}>
                 {t("col.selectAll")}
               </Button>
               <Button type="button" variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => setRel({ m2mDisplayColumns: [] })}>
                 {t("col.clear")}
               </Button>
             </div>
           </div>
          <div className="flex flex-wrap gap-1.5">
            {targetCols.map((c) => {
              const on = selectedDisplay.includes(c.name);
              return (
                <button
                  key={c.name}
                  type="button"
                  onClick={() =>
                    setRel({
                      m2mDisplayColumns: on
                        ? selectedDisplay.filter((d) => d !== c.name)
                        : [...selectedDisplay, c.name],
                    })
                  }
                  className={`rounded-md border px-2 py-1 text-[10px] font-mono transition-colors ${
                    on
                      ? "border-violet-500/40 bg-violet-500/10 text-violet-700 dark:text-violet-300"
                      : "border-border text-muted-foreground hover:border-violet-500/30"
                  }`}
                >
                  {c.name}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

function defsTableName(id: string, defs: TableDefPayload[]): string {
  return defs.find((d) => String(d.id) === id)?.tableName?.replace(/[^A-Za-z0-9_]/g, "_") ?? "j";
}
