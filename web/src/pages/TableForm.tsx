import { useEffect, useState } from "react";
import { useNavigate, useParams, Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Server,
  Table2,
  CheckCircle2,
  Lock,
  Save,
  Layers,
  Database,
  Key,
} from "lucide-react";
import { api } from "../lib/api";
import type { BaseFieldType, ColumnDef, Datasource, LiveColumn, TableDefPayload } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { ColumnListEditor, type FormCol } from "../components/ColumnListEditor";

export const fieldTypes = ["boolean", "text", "number", "datetime", "enum", "uuid", "json", "fk"] as const;

export function normalizeLabel(name: string): string {
  if (!name) return "";
  return name
    .replace(/[-_]+/g, " ")
    .trim()
    .split(/\s+/)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
    .join(" ");
}

export default function TableForm() {
  const { id } = useParams();
  const isEditing = !!id;
  const navigate = useNavigate();
  const qc = useQueryClient();

  const [dsId, setDsId] = useState("");
  const [schema, setSchema] = useState("");
  const [tableName, setTableName] = useState("");
  const [label, setLabel] = useState("");
  const [pageSize, setPageSize] = useState(20);
  const [defaultSortCol, setDefaultSortCol] = useState("");
  const [defaultSortDir, setDefaultSortDir] = useState<"ASC" | "DESC">("ASC");
  const [keys, setKeys] = useState<string[]>([]);
  const [cols, setCols] = useState<FormCol[]>([]);

  // Existing definition query for Edit mode
  const existingDef = useQuery({
    queryKey: ["def", id],
    enabled: isEditing,
    queryFn: () => api<TableDefPayload>(`/tables/${id}`),
  });

  // Datasources list query
  const dsList = useQuery({
    queryKey: ["ds"],
    queryFn: () => api<Datasource[]>("/datasources"),
  });

  // Ku-CRUD table definitions list (for FK target selection)
  const defs = useQuery({
    queryKey: ["defs"],
    queryFn: () => api<TableDefPayload[]>("/tables"),
  });

  // Database tables query
  const tables = useQuery({
    queryKey: ["ds-tables", dsId],
    enabled: !!dsId,
    queryFn: () => api<{ schema: string; name: string }[]>(`/datasources/${dsId}/tables`),
  });

  // Live database columns query for New mode
  const liveCols = useQuery({
    queryKey: ["ds-cols", dsId, schema, tableName],
    enabled: !isEditing && !!dsId && !!schema && !!tableName,
    queryFn: () => api<LiveColumn[]>(`/datasources/${dsId}/tables/${schema}/${tableName}/columns`),
  });

  // Populate form state when editing existing definition
  useEffect(() => {
    if (isEditing && existingDef.data) {
      const d = existingDef.data;
      setDsId(String(d.datasourceId));
      setSchema(d.schemaName);
      setTableName(d.tableName);
      setLabel(d.label);
      setPageSize(d.pageSize);
      setDefaultSortCol(d.defaultSortCol ?? "");
      setDefaultSortDir(d.defaultSortDir === "DESC" ? "DESC" : "ASC");
      setKeys(d.keyColumns ?? []);
      setCols(d.columns);
    }
  }, [isEditing, existingDef.data]);

  // Populate columns from live DB inspection when creating new definition
  useEffect(() => {
    if (!isEditing && liveCols.data && liveCols.data.length > 0) {
      setLabel(normalizeLabel(tableName));
      setKeys((prev) => (prev.length ? prev : liveCols.data.filter((c) => c.isPk).map((c) => c.name)));
      setCols(
        liveCols.data.map((c, i) => {
          const isNotNull = !c.nullable;
          return {
            name: c.name,
            label: normalizeLabel(c.name),
            fieldType: c.fieldType,
            enumOptions: c.enumOptions,
            editable: isNotNull ? true : !c.isPk,
            required: isNotNull,
            visible: true,
            searchable: true,
            sortable: true,
            position: i,
            livePk: c.isPk,
            origType: c.fieldType as BaseFieldType,
            fkDs: dsId,
          };
        })
      );
    }
  }, [isEditing, liveCols.data, tableName, dsId]);

  const setCol = (i: number, patch: Partial<FormCol>) =>
    setCols((prev) => prev.map((c, j) => (j === i ? { ...c, ...patch } : c)));

  const save = useMutation({
    mutationFn: () => {
      const body = JSON.stringify({
        datasourceId: dsId,
        schemaName: schema,
        tableName: tableName,
        label,
        keyColumns: keys,
        pageSize,
        defaultSortCol,
        defaultSortDir,
        columns: cols.map(({ livePk: _lp, origType: _ot, fkDs: _fd, ...c }) =>
          c.fieldType === "fk"
            ? c
            : { ...c, baseType: undefined, fkTableDefId: undefined, fkRefColumn: undefined, fkDisplayColumns: undefined }
        ),
      });
      return isEditing
        ? api(`/tables/${id}`, { method: "PUT", body })
        : api("/tables", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["defs"] });
      navigate("/");
    },
  });

  const step1Complete = !!dsId;
  const step2Complete = step1Complete && !!schema && !!tableName;
  const keysValid = keys.length > 0 && keys.every((k) => cols.some((c) => c.name === k));
  const toggleKey = (name: string) =>
    setKeys((prev) => (prev.includes(name) ? prev.filter((k) => k !== name) : [...prev, name]));

  if (isEditing && existingDef.isLoading) {
    return (
      <div className="flex h-64 items-center justify-center text-xs text-muted-foreground">
        Loading table definition...
      </div>
    );
  }

  return (
    <div className="space-y-6 pb-12">
      {/* Top Header Bar */}
      <div className="flex items-center justify-between border-b pb-4">
        <div className="flex items-center gap-3">
          <Link to="/">
            <Button variant="outline" size="icon" className="h-9 w-9">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div>
            <h2 className="text-xl font-bold tracking-tight">
              {isEditing ? `Edit Definition: ${label}` : "Create Table Definition"}
            </h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              {isEditing
                ? `Modifying mapping for definition #${id}`
                : "Map a database table into Ku-CRUD through progressive step configuration"}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Link to="/">
            <Button variant="outline" className="h-9 text-xs">
              Cancel
            </Button>
          </Link>
          <Button
            disabled={!step2Complete || !keysValid || save.isPending}
            onClick={() => save.mutate()}
            className="h-9 bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1.5 text-xs"
          >
            <Save className="h-4 w-4" />
            {save.isPending ? "Saving..." : isEditing ? "Save Changes" : "Create Definition"}
          </Button>
        </div>
      </div>

      {/* SECTION 1: Datasource Selection */}
      <Card className={`border-border/60 transition-all ${!step1Complete ? "ring-2 ring-blue-500/20" : ""}`}>
        <CardHeader className="pb-3 border-b">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <div className={`flex h-8 w-8 items-center justify-center rounded-lg ${step1Complete ? "bg-emerald-500/10 text-emerald-500" : "bg-blue-500/10 text-blue-500"}`}>
                {step1Complete ? <CheckCircle2 className="h-4 w-4" /> : <Server className="h-4 w-4" />}
              </div>
              <div>
                <CardTitle className="text-sm font-semibold">Step 1: Select Datasource Connection</CardTitle>
                <CardDescription className="text-xs">Choose the active database connection pool</CardDescription>
              </div>
            </div>
            {isEditing && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> Locked on Edit
              </span>
            )}
          </div>
        </CardHeader>
        <CardContent className="pt-4">
          <div className="max-w-xl space-y-2">
            <Label className="text-xs font-medium">Datasource Connection</Label>
            <Select
              value={dsId}
              disabled={isEditing}
              onValueChange={(v) => {
                setDsId(v);
                setSchema("");
                setTableName("");
                setCols([]);
              }}
            >
              <SelectTrigger className="h-10 text-xs">
                <SelectValue placeholder="Choose a database connection..." />
              </SelectTrigger>
              <SelectContent>
                {(dsList.data ?? []).map((d) => (
                  <SelectItem key={d.id} value={String(d.id)} className="text-xs">
                    <div className="flex items-center gap-2">
                      <Server className="h-3.5 w-3.5 text-blue-500" />
                      <span className="font-medium">{d.name}</span>
                      <span className="text-muted-foreground">({d.dbname})</span>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {(dsList.data ?? []).length === 0 && (
              <p className="text-xs text-amber-600 dark:text-amber-400 bg-amber-500/10 p-3 rounded-lg border border-amber-500/20 mt-2">
                No datasources available. Please add a Datasource connection first.
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {/* SECTION 2: Select Table */}
      <Card className={`border-border/60 transition-all ${!step1Complete ? "opacity-60 pointer-events-none bg-muted/20" : step2Complete ? "" : "ring-2 ring-blue-500/20"}`}>
        <CardHeader className="pb-3 border-b">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <div className={`flex h-8 w-8 items-center justify-center rounded-lg ${step2Complete ? "bg-emerald-500/10 text-emerald-500" : "bg-blue-500/10 text-blue-500"}`}>
                {step2Complete ? <CheckCircle2 className="h-4 w-4" /> : <Table2 className="h-4 w-4" />}
              </div>
              <div>
                <CardTitle className="text-sm font-semibold">Step 2: Select Table from Schema</CardTitle>
                <CardDescription className="text-xs">Pick target database table to generate CRUD mappings</CardDescription>
              </div>
            </div>
            {!step1Complete && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> Select Datasource first
              </span>
            )}
            {isEditing && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> Locked on Edit
              </span>
            )}
          </div>
        </CardHeader>
        <CardContent className="pt-4 space-y-4">
          <fieldset disabled={!step1Complete || isEditing} className="max-w-xl space-y-2">
            <Label className="text-xs font-medium">Database Table</Label>
            <Select
              value={schema && tableName ? `${schema}.${tableName}` : ""}
              onValueChange={(v) => {
                const [s, t] = v.split(".");
                setSchema(s);
                setTableName(t);
                setLabel(normalizeLabel(t));
                setCols([]);
                setKeys([]);
              }}
            >
              <SelectTrigger className="h-10 text-xs">
                <SelectValue placeholder="Choose a table..." />
              </SelectTrigger>
              <SelectContent>
                {(tables.data ?? []).map((t) => (
                  <SelectItem key={`${t.schema}.${t.name}`} value={`${t.schema}.${t.name}`} className="text-xs font-mono">
                    {t.schema}.{t.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </fieldset>

          {/* Introspected Database Schema Preview */}
          {step2Complete && (
            <div className="space-y-2 pt-2 border-t border-border/40">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-blue-500" />
                  <span className="text-xs font-semibold text-foreground">
                    Inspected Database Schema ({schema}.{tableName})
                  </span>
                </div>
                <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
                  {isEditing ? cols.length : (liveCols.data?.length ?? 0)} Raw DB Columns Introspected
                </Badge>
              </div>

              {liveCols.isLoading && !isEditing ? (
                <div className="p-4 text-center text-xs text-muted-foreground italic rounded-lg border bg-muted/20">
                  Inspecting raw table schema and data types from database...
                </div>
              ) : (
                <div className="rounded-lg border bg-muted/10 overflow-hidden max-h-56 overflow-y-auto shadow-xs">
                  <Table>
                    <TableHeader>
                      <TableRow className="bg-muted/50 hover:bg-muted/50 text-[11px]">
                        <TableHead className="py-2 h-8 font-semibold">Raw Column Name</TableHead>
                        <TableHead className="py-2 h-8 font-semibold">DB Data Type</TableHead>
                        <TableHead className="py-2 h-8 font-semibold text-center">Attributes / Constraints</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {(isEditing ? cols : (liveCols.data ?? [])).map((col) => {
                        const colName = col.name;
                        const colType = col.fieldType;
                        const isPk = isEditing ? keys.includes(col.name) : (col as LiveColumn).isPk;
                        const isNullable = isEditing ? !(col as ColumnDef).required : (col as LiveColumn).nullable;

                        return (
                          <TableRow key={colName} className="hover:bg-muted/30 text-xs">
                            <TableCell className="py-1.5 font-mono font-bold text-foreground">
                              {colName}
                            </TableCell>
                            <TableCell className="py-1.5">
                              <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20">
                                {colType}
                              </Badge>
                            </TableCell>
                            <TableCell className="py-1.5 text-center">
                              <div className="flex items-center justify-center gap-1.5 flex-wrap">
                                {isPk && (
                                  <Badge variant="outline" className="text-[9px] font-mono bg-amber-500/10 text-amber-600 border-amber-500/20 gap-0.5">
                                    <Key className="h-2.5 w-2.5" /> Primary Key
                                  </Badge>
                                )}
                                <Badge variant="outline" className={`text-[9px] font-mono ${isNullable ? "bg-muted/60 text-muted-foreground" : "bg-emerald-500/10 text-emerald-600 border-emerald-500/20"}`}>
                                  {isNullable ? "NULLABLE" : "NOT NULL"}
                                </Badge>
                              </div>
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* SECTION 3: Configure Label, PK & Columns */}
      <Card className={`border-border/60 transition-all ${!step2Complete ? "opacity-60 pointer-events-none bg-muted/20" : ""}`}>
        <CardHeader className="pb-3 border-b">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <div className={`flex h-8 w-8 items-center justify-center rounded-lg ${keysValid ? "bg-emerald-500/10 text-emerald-500" : "bg-blue-500/10 text-blue-500"}`}>
                <Layers className="h-4 w-4" />
              </div>
              <div>
                <CardTitle className="text-sm font-semibold">Step 3: Configure Columns & Display Parameters</CardTitle>
                <CardDescription className="text-xs">Set display label, key column(s), and column property switches</CardDescription>
              </div>
            </div>
            {!step2Complete && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> Select Table first
              </span>
            )}
          </div>
        </CardHeader>

        <CardContent className="pt-4 space-y-6">
          <fieldset disabled={!step2Complete} className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">Display Label</Label>
                <Input
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  placeholder="e.g. User Accounts"
                  className="h-9 text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">Page Size (Rows per Page)</Label>
                <Input
                  type="number"
                  value={pageSize}
                  onChange={(e) => setPageSize(Number(e.target.value))}
                  className="h-9 text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">Default Sort Column</Label>
                <Select
                  value={defaultSortCol || "none"}
                  onValueChange={(v) => setDefaultSortCol(v === "none" ? "" : v)}
                >
                  <SelectTrigger className="h-9 text-xs">
                    <SelectValue placeholder="None (key column)" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none" className="text-xs">None (key column)</SelectItem>
                    {cols.filter((c) => c.sortable).map((c) => (
                      <SelectItem key={c.name} value={c.name} className="text-xs">
                        {c.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">Default Sort Direction</Label>
                <Select
                  value={defaultSortDir}
                  onValueChange={(v) => setDefaultSortDir(v === "DESC" ? "DESC" : "ASC")}
                  disabled={!defaultSortCol}
                >
                  <SelectTrigger className="h-9 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ASC" className="text-xs">Ascending</SelectItem>
                    <SelectItem value="DESC" className="text-xs">Descending</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Sequential Expandable Column Editor */}
            <ColumnListEditor
              cols={cols}
              keys={keys}
              toggleKey={toggleKey}
              setCol={setCol}
              dsId={dsId}
              currentId={id}
              defs={defs.data ?? []}
              dsList={dsList.data ?? []}
              isLoadingCols={liveCols.isLoading}
            />

            {save.isError && (
              <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive">
                {String((save.error as Error).message)}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-4 border-t">
              <Link to="/">
                <Button variant="outline">Cancel</Button>
              </Link>
              <Button
                disabled={!step2Complete || !keysValid || save.isPending}
                onClick={() => save.mutate()}
                className="bg-blue-600 text-white hover:bg-blue-700 gap-1.5"
              >
                <Save className="h-4 w-4" />
                {save.isPending ? "Saving..." : isEditing ? "Save Changes" : keysValid ? "Create Table Definition" : "Select at Least One Key Column"}
              </Button>
            </div>
          </fieldset>
        </CardContent>
      </Card>
    </div>
  );
}

/** Backup Legacy Column Table Component (Retained as backup) */
/* eslint-disable @typescript-eslint/no-unused-vars */
export function LegacyColumnTable({
  cols, keys, toggleKey, setCol, isLoadingCols, fieldTypes,
}: {
  cols: FormCol[]; keys: string[]; toggleKey: (name: string) => void;
  setCol: (i: number, patch: Partial<FormCol>) => void; isLoadingCols: boolean;
  fieldTypes: readonly string[];
}) {
  return (
    <div className="rounded-lg border bg-card overflow-hidden shadow-xs">
      <div className="overflow-x-auto max-h-[500px]">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/50">
              <TableHead className="w-12 text-center">Key</TableHead>
              <TableHead>Column Name</TableHead>
              <TableHead className="w-48">Display Label</TableHead>
              <TableHead className="w-36">Field Type</TableHead>
              <TableHead className="text-center">Editable</TableHead>
              <TableHead className="text-center">Required</TableHead>
              <TableHead className="text-center">Visible</TableHead>
              <TableHead className="text-center">Searchable</TableHead>
              <TableHead className="text-center">Sortable</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cols.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="h-32 text-center text-xs text-muted-foreground">
                  {isLoadingCols ? "Inspecting live table columns..." : "No column mappings populated"}
                </TableCell>
              </TableRow>
            ) : (
              cols.map((c, i) => (
                <TableRow key={c.name} className="hover:bg-muted/20">
                  <TableCell className="text-center">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-blue-600 cursor-pointer"
                      checked={keys.includes(c.name)}
                      onChange={() => toggleKey(c.name)}
                    />
                  </TableCell>
                  <TableCell className="font-mono text-xs font-medium">{c.name}</TableCell>
                  <TableCell>
                    <Input value={c.label} className="h-8 text-xs" onChange={(e) => setCol(i, { label: e.target.value })} />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={c.fieldType}
                      onValueChange={(v) => setCol(i, { fieldType: v as ColumnDef["fieldType"] })}
                    >
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {fieldTypes.map((t) => (
                          <SelectItem key={t} value={t} className="text-xs">{t}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell className="text-center"><Switch checked={c.editable} onCheckedChange={(v) => setCol(i, { editable: v })} /></TableCell>
                  <TableCell className="text-center"><Switch checked={c.required} onCheckedChange={(v) => setCol(i, { required: v })} /></TableCell>
                  <TableCell className="text-center"><Switch checked={c.visible} onCheckedChange={(v) => setCol(i, { visible: v })} /></TableCell>
                  <TableCell className="text-center"><Switch checked={c.searchable} onCheckedChange={(v) => setCol(i, { searchable: v })} /></TableCell>
                  <TableCell className="text-center"><Switch checked={c.sortable} onCheckedChange={(v) => setCol(i, { sortable: v })} /></TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
