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
import type { BaseFieldType, ColumnDef, Datasource, TableDefPayload, ValidationRuleType } from "../lib/types";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const fieldTypes = ["boolean", "text", "number", "datetime", "enum", "uuid", "json", "fk"] as const;

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
        aria-label="Help information"
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
}: ColumnListEditorProps) {
  // Store expanded state per column name
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

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
        Inspecting live table columns...
      </div>
    );
  }

  if (cols.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center rounded-lg border bg-card text-xs text-muted-foreground">
        No column mappings populated
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Step 3 Header Help & Counter */}
      <div className="flex items-center justify-between px-1 text-xs text-muted-foreground font-medium">
        <div className="flex items-center gap-1.5">
          <span>Column Configuration List ({cols.length} columns)</span>
          <HelpPopover title="Column Configuration Guide">
            <p>
              Configure display properties and behaviors for each database column mapped to Ku-CRUD.
            </p>
            <p className="pt-1">
              Each column allows setting a Display Label, Primary Key status, UI field type, and feature switches (Editable, Required, Visible, Searchable, Sortable).
            </p>
          </HelpPopover>
        </div>
        <span>Click settings icon to configure Enum / Foreign Key relations</span>
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
                      title="Use as Primary / Composite Key"
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
                      <HelpPopover title="Composite Key & Query Logic">
                        <p>
                          <strong>Primary / Composite Key</strong> serves as the unique record identifier.
                        </p>
                        <p className="pt-1">
                          <strong>Backend Logic:</strong> Key column values are used in <code>WHERE</code> clauses for <code>UPDATE</code> and <code>DELETE</code> queries.
                        </p>
                        <p className="pt-1 text-[10px] italic">
                          Check multiple columns if the table uses a Composite Key (e.g. <code>dept_id + emp_id</code>).
                        </p>
                      </HelpPopover>
                    </div>
                  </div>

                  {/* Display Label & Field Type Select */}
                  <div className="flex flex-wrap items-center gap-2 flex-1">
                    {/* Display Label Input */}
                    <div className="space-y-1 flex-1 min-w-[150px]">
                      <div className="flex items-center gap-1">
                        <Label className="text-[10px] text-muted-foreground font-medium">Display Label</Label>
                        <HelpPopover title="Display Label">
                          Human-readable name shown on table headers and CRUD form labels instead of raw database column names.
                        </HelpPopover>
                      </div>
                      <Input
                        value={c.label}
                        onChange={(e) => setCol(i, { label: e.target.value })}
                        className="h-8 text-xs bg-muted/20"
                        placeholder="Column label"
                      />
                    </div>

                    {/* Field Type Select */}
                    <div className="space-y-1 w-36">
                      <div className="flex items-center gap-1">
                        <Label className="text-[10px] text-muted-foreground font-medium">Field Type</Label>
                        <HelpPopover title="Field Type Guide">
                          <p className="mb-1">Form input component types:</p>
                          <ul className="space-y-1 list-disc pl-3 text-[10px]">
                            <li><strong>boolean</strong>: True/False toggle switch.</li>
                            <li><strong>text</strong>: Standard text input (string).</li>
                            <li><strong>number</strong>: Numeric input (int/float).</li>
                            <li><strong>datetime</strong>: Date & Time picker.</li>
                            <li><strong>enum</strong>: Dropdown of predefined options.</li>
                            <li><strong>fk</strong>: Foreign Key relation with search modal.</li>
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
                        <PropertyToggle label="Edit" checked={c.editable} onChange={(v) => handleColPatch(i, { editable: v })} />
                        <PropertyToggle label="Req" checked={c.required} onChange={(v) => handleColPatch(i, { required: v })} />
                        <PropertyToggle label="Vis" checked={c.visible} onChange={(v) => handleColPatch(i, { visible: v })} />
                        <PropertyToggle label="Srch" checked={c.searchable} onChange={(v) => handleColPatch(i, { searchable: v })} />
                        <PropertyToggle label="Sort" checked={c.sortable} onChange={(v) => handleColPatch(i, { sortable: v })} />
                      </div>
                      {/* Property Switch Tooltip */}
                      <HelpPopover title="Column Switches & Form Policy">
                        <ul className="space-y-1.5 text-[10px]">
                          <li><strong>Edit (Editable)</strong>: Controls form field availability. Turn on for PK columns if manual ID input is required.</li>
                          <li><strong>Req (Required)</strong>: Mandatory field (cannot be empty or NULL).</li>
                          <li><strong>Vis (Visible)</strong>: Displayed as a column on the Data CRUD grid.</li>
                          <li><strong>Srch (Searchable)</strong>: Included in search bar text queries.</li>
                          <li><strong>Sort (Sortable)</strong>: Can be sorted (ASC/DESC) when column header is clicked.</li>
                        </ul>
                        <div className="pt-1.5 mt-1 border-t border-border/40 text-[10px] text-muted-foreground">
                          💡 <strong>PK Form Policy:</strong> Primary Key fields appear on the Insert Form for manual ID entry, and are automatically displayed as Read-Only on the Edit Form to protect record identity.
                        </div>
                      </HelpPopover>
                    </div>

                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className={`h-8 px-2 text-xs gap-1 shrink-0 ${hasCustomConfig ? "text-blue-600 dark:text-blue-400" : "text-muted-foreground"}`}
                      onClick={() => toggleExpand(c.name)}
                      title="Toggle detailed column configuration"
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

                  {c.editable && c.fieldType !== "m2m" && c.fieldType !== "fk" && (
                    <ValidationsEditor col={c} index={i} setCol={setCol} />
                  )}

                  {c.fieldType !== "enum" && c.fieldType !== "fk" && (
                    <div className="text-xs text-muted-foreground flex items-center gap-2 italic">
                      <span>Standard properties for <strong className="font-mono">{c.fieldType}</strong> type are configured in controls above. No additional configuration needed.</span>
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
  return (
    <div className="flex flex-col items-center gap-1" title={disabled ? "Disabled" : undefined}>
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
      <span className="text-[11px] font-medium text-muted-foreground">Validations:</span>
      {(["email", "number", "text"] as const).map((t) => (
        <label key={t} className="flex items-center gap-1 text-xs">
          <Checkbox checked={has(t)} onChange={(e) => set(t, e.target.checked)} />
          {t === "email" ? "Email" : t === "number" ? "Number only" : "Text only"}
        </label>
      ))}
      {(["min_len", "max_len"] as const).map((t) => (
        <label key={t} className="flex items-center gap-1 text-xs">
          <Checkbox checked={has(t)} onChange={(e) => set(t, e.target.checked, param(t) || 1)} />
          {t === "min_len" ? "Min" : "Max"} len
          <Input type="number" className="h-6 w-16" min={1} max={1000} disabled={!has(t)}
            value={has(t) ? param(t) : ""} onChange={(e) => set(t, true, Number(e.target.value) || 1)} />
        </label>
      ))}
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
  const options = col.enumOptions ?? [];
  const rawInput = options.join(", ");

  return (
    <div className="space-y-2 rounded-lg border border-purple-500/20 bg-purple-500/5 p-3.5">
      <div className="flex items-center gap-2">
        <ListFilter className="h-4 w-4 text-purple-600 dark:text-purple-400" />
        <Label className="text-xs font-semibold text-purple-900 dark:text-purple-300">
          Enum Options Configuration ({col.name})
        </Label>
        <HelpPopover title="Enum Type Guide">
          Enum options restrict form inputs to specified values. Enter a list of values separated by commas (e.g., <code>ACTIVE, INACTIVE, PENDING</code>).
        </HelpPopover>
      </div>
      <p className="text-[11px] text-muted-foreground">
        Enter allowed enum option values for this column (comma-separated).
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
          <span className="text-[10px] text-muted-foreground font-medium">Parsed options:</span>
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
            Foreign Key Relation Configuration ({col.name})
          </Label>
          <HelpPopover title="Foreign Key (FK) Guide">
            <p>
              Foreign Key links this column to records in another defined table.
            </p>
            <p className="pt-1">
              Users can select related records via an interactive search modal when inserting or editing.
            </p>
          </HelpPopover>
        </div>
        <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
          Target Ref: {col.fkRefColumn || "—"}
        </Badge>
      </div>

      {/* Primary Target Selectors (3 Columns) */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {/* Target Datasource */}
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">Target Datasource</Label>
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
          <Label className="text-[11px] text-muted-foreground font-medium">Related Table</Label>
          <Select
            value={col.fkTableDefId ?? ""}
            onValueChange={(v) =>
              setCol(index, { fkTableDefId: v, fkRefColumn: undefined, fkDisplayColumns: undefined })
            }
          >
            <SelectTrigger className="h-9 text-xs bg-background">
              <SelectValue placeholder="Choose related table..." />
            </SelectTrigger>
            <SelectContent>
              {fkDs === dsId && (
                <SelectItem value="self" className="text-xs">
                  This table (self)
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
          <Label className="text-[11px] text-muted-foreground font-medium">Reference Column (Target PK / Key Column)</Label>
          <Select
            value={col.fkRefColumn ?? ""}
            onValueChange={(v) => setCol(index, { fkRefColumn: v, fkDisplayColumns: undefined })}
            disabled={!col.fkTableDefId}
          >
            <SelectTrigger className="h-9 text-xs bg-background font-mono">
              <SelectValue placeholder="Select reference column..." />
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
              Display Columns (UI fields shown when relation is picked)
            </Label>
            <Badge variant="secondary" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
              {selectedDisplayCols.length} of {availableDisplayCols.length} selected
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
                Select All
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 text-[11px] px-2 text-muted-foreground hover:text-destructive"
                onClick={handleClearAll}
              >
                Clear All
              </Button>
            </div>
          )}
        </div>

        {!col.fkTableDefId ? (
          <div className="p-3 text-center text-xs text-muted-foreground italic rounded-md border border-dashed bg-background/50">
            Please select a <strong>Related Table</strong> first to pick display columns.
          </div>
        ) : availableDisplayCols.length === 0 ? (
          <div className="p-3 text-center text-xs text-muted-foreground italic rounded-md border border-dashed bg-background/50">
            No additional columns available on this table.
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
        label: "New Relation",
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
            Many-to-Many Relations
          </Label>
          <HelpPopover title="Many-to-Many Guide">
            <p>A relation connects this table to another through a <strong>junction table</strong>.</p>
            <p className="pt-1">The junction must already be defined as a table definition with two fk columns: one pointing at this table, one at the target.</p>
            <p className="pt-1 text-[10px]">💡 Save this table definition first — relations reference it by id, so they can only be configured when editing an existing definition.</p>
          </HelpPopover>
        </div>
        <Button type="button" variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={addRel} disabled={!currentId}>
          <Plus className="h-3.5 w-3.5" /> Add Relation
        </Button>
      </div>
      {!currentId && (
        <p className="text-[11px] text-muted-foreground">
          Save the definition first, then edit it to add many-to-many relations.
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
        <Input
          className="h-8 flex-1 text-xs"
          value={rel.label}
          onChange={(e) => setRel({ label: e.target.value })}
          placeholder="Relation label (e.g. Tags)"
        />
        <Button type="button" variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-destructive" onClick={remove} title="Remove relation">
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">Junction Table</Label>
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
              <SelectValue placeholder="Choose junction table..." />
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
          <Label className="text-[11px] text-muted-foreground font-medium">Link to this table via</Label>
          <Select
            value={rel.m2mJunctionSrcCol ?? ""}
            onValueChange={(v) => setRel({ m2mJunctionSrcCol: v, m2mJunctionTgtCol: undefined, m2mDisplayColumns: [] })}
            disabled={!rel.m2mJunctionDefId}
          >
            <SelectTrigger className="h-8 text-xs bg-background">
              <SelectValue placeholder="Junction fk column → this table" />
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
            <p className="text-[10px] text-amber-600">No junction fk column points at this table yet.</p>
          )}
        </div>
        <div className="space-y-1">
          <Label className="text-[11px] text-muted-foreground font-medium">Related table via</Label>
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
              <SelectValue placeholder="Junction fk column → target" />
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
            <Label className="text-[11px] text-muted-foreground font-medium">Display columns on target table</Label>
            <div className="flex gap-1">
              <Button type="button" variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => setRel({ m2mDisplayColumns: targetCols.map((c) => c.name) })}>
                Select All
              </Button>
              <Button type="button" variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => setRel({ m2mDisplayColumns: [] })}>
                Clear
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
