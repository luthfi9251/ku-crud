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
import type { BaseFieldType, ColumnDef, Datasource, HooksConfig, HooksListRes, LiveColumn, TableDefPayload, ViewConfig } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { ColumnListEditor, M2MRelationsEditor, HelpPopover, type FormCol } from "../components/ColumnListEditor";
import HooksEditor from "../components/HooksEditor";
import { useT } from "../lib/i18n";

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
  const t = useT();

  const [dsId, setDsId] = useState("");
  const [schema, setSchema] = useState("");
  const [tableName, setTableName] = useState("");
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [pageSize, setPageSize] = useState(20);
  const [defaultSortCol, setDefaultSortCol] = useState("");
  const [defaultSortDir, setDefaultSortDir] = useState<"ASC" | "DESC">("ASC");
  const [keys, setKeys] = useState<string[]>([]);
  const [cols, setCols] = useState<FormCol[]>([]);
  const [defaultView, setDefaultView] = useState<"grid" | "kanban" | "calendar">("grid");
  const [viewConfig, setViewConfig] = useState<ViewConfig>({});
  const [hooksCfg, setHooksCfg] = useState<HooksConfig>({});

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

  // Compiled-in hook registry (Platform-gated; failure just hides the editor)
  const hookNames = useQuery({
    queryKey: ["hooks"],
    queryFn: () => api<HooksListRes>("/hooks"),
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
      setDescription(d.description ?? "");
      setPageSize(d.pageSize);
      setDefaultSortCol(d.defaultSortCol ?? "");
      setDefaultSortDir(d.defaultSortDir === "DESC" ? "DESC" : "ASC");
      setDefaultView(d.defaultView ?? "grid");
      setViewConfig(d.viewConfig ?? {});
      setHooksCfg(d.hooks ?? {});
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
        description,
        keyColumns: keys,
        pageSize,
        defaultSortCol,
        defaultSortDir,
        defaultView,
        viewConfig,
        hooks: Object.keys(hooksCfg).length ? hooksCfg : undefined,
        columns: cols.map(({ livePk: _lp, origType: _ot, fkDs: _fd, ...c }) =>
          c.fieldType === "fk"
            ? { ...c, m2mJunctionDefId: undefined, m2mJunctionSrcCol: undefined, m2mJunctionTgtCol: undefined, m2mDisplayColumns: undefined, m2mRefColumn: undefined }
            : c.fieldType === "m2m"
              ? { ...c, baseType: undefined, fkTableDefId: undefined, fkRefColumn: undefined, fkDisplayColumns: undefined, m2mRefColumn: undefined }
              : { ...c, baseType: undefined, fkTableDefId: undefined, fkRefColumn: undefined, fkDisplayColumns: undefined,
                  m2mJunctionDefId: undefined, m2mJunctionSrcCol: undefined, m2mJunctionTgtCol: undefined, m2mDisplayColumns: undefined, m2mRefColumn: undefined }
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
        {t("tform.loading")}
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
              {isEditing ? t("tform.editTitle", { label }) : t("tform.createTitle")}
            </h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              {isEditing
                ? t("tform.editSub", { id: String(id) })
                : t("tform.createSub")}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Link to="/">
            <Button variant="outline" className="h-9 text-xs">
              {t("form.cancel")}
            </Button>
          </Link>
          <Button
            disabled={!step2Complete || !keysValid || save.isPending}
            onClick={() => save.mutate()}
            className="h-9 bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1.5 text-xs"
          >
            <Save className="h-4 w-4" />
            {save.isPending ? t("form.saving") : isEditing ? t("tform.saveChanges") : t("tform.createDef")}
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
                <CardTitle className="text-sm font-semibold">{t("tform.step1")}</CardTitle>
                <CardDescription className="text-xs">{t("tform.step1Desc")}</CardDescription>
              </div>
            </div>
            {isEditing && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> {t("tform.locked")}
              </span>
            )}
          </div>
        </CardHeader>
        <CardContent className="pt-4">
          <div className="max-w-xl space-y-2">
            <Label className="text-xs font-medium">{t("tform.dsConn")}</Label>
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
                <SelectValue placeholder={t("tform.dsPh")} />
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
                {t("tform.noDs")}
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
                <CardTitle className="text-sm font-semibold">{t("tform.step2")}</CardTitle>
                <CardDescription className="text-xs">{t("tform.step2Desc")}</CardDescription>
              </div>
            </div>
            {!step1Complete && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> {t("tform.selectDsFirst")}
              </span>
            )}
            {isEditing && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> {t("tform.locked")}
              </span>
            )}
          </div>
        </CardHeader>
        <CardContent className="pt-4 space-y-4">
          <fieldset disabled={!step1Complete || isEditing} className="max-w-xl space-y-2">
            <Label className="text-xs font-medium">{t("tform.dbTable")}</Label>
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
                <SelectValue placeholder={t("tform.tablePh")} />
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
                    {t("tform.inspected", { name: `${schema}.${tableName}` })}
                  </span>
                </div>
                <Badge variant="outline" className="text-[10px] font-mono bg-blue-500/10 text-blue-600 border-blue-500/20">
                  {t("tform.rawCols", { count: String(isEditing ? cols.length : (liveCols.data?.length ?? 0)) })}
                </Badge>
              </div>

              {liveCols.isLoading && !isEditing ? (
                <div className="p-4 text-center text-xs text-muted-foreground italic rounded-lg border bg-muted/20">
                  {t("tform.inspecting")}
                </div>
              ) : (
                <div className="rounded-lg border bg-muted/10 overflow-hidden max-h-56 overflow-y-auto shadow-xs">
                  <Table>
                    <TableHeader>
                      <TableRow className="bg-muted/50 hover:bg-muted/50 text-[11px]">
                         <TableHead className="py-2 h-8 font-semibold">{t("tform.colRawName")}</TableHead>
                         <TableHead className="py-2 h-8 font-semibold">{t("tform.colDbType")}</TableHead>
                         <TableHead className="py-2 h-8 font-semibold text-center">{t("tform.colAttrs")}</TableHead>
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
                                     <Key className="h-2.5 w-2.5" /> {t("tform.primaryKey")}
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
                <CardTitle className="text-sm font-semibold">{t("tform.step3")}</CardTitle>
                <CardDescription className="text-xs">{t("tform.step3Desc")}</CardDescription>
              </div>
            </div>
            {!step2Complete && (
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono bg-muted/60 px-2 py-0.5 rounded">
                <Lock className="h-3 w-3" /> {t("tform.selectTableFirst")}
              </span>
            )}
          </div>
        </CardHeader>

        <CardContent className="pt-4 space-y-6">
          <fieldset disabled={!step2Complete} className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
               <div className="space-y-1.5 md:col-span-2">
                 <Label className="text-xs font-medium">{t("tform.displayLabel")}</Label>
                 <Input
                   value={label}
                   onChange={(e) => setLabel(e.target.value)}
                   placeholder={t("tform.labelPh")}
                   className="h-9 text-xs"
                 />
                 <p className="text-[11px] text-muted-foreground">{t("tform.labelHint")}</p>
                 <div className="flex items-center gap-2.5 rounded-lg border bg-sidebar px-3 py-2 w-fit">
                   <Layers className="h-3.5 w-3.5 text-sidebar-foreground/50" />
                   <span className="text-xs text-sidebar-foreground/80 truncate max-w-[240px]">
                     {label || t("tform.labelPh")}
                   </span>
                 </div>
               </div>
               <div className="space-y-1.5 md:col-span-2">
                 <Label className="text-xs font-medium">{t("tform.description")}</Label>
                 <Textarea
                   value={description}
                   onChange={(e) => setDescription(e.target.value)}
                   maxLength={200}
                   placeholder={t("tform.descPh")}
                   className="text-xs min-h-[64px]"
                 />
                 <p className="text-[11px] text-muted-foreground">{t("tform.descHint")}</p>
               </div>
               <div className="space-y-1.5">
                 <Label className="text-xs font-medium">{t("tform.pageSize")}</Label>
                <Input
                  type="number"
                  value={pageSize}
                  onChange={(e) => setPageSize(Number(e.target.value))}
                  className="h-9 text-xs"
                />
              </div>
               <div className="space-y-1.5">
                 <Label className="text-xs font-medium">{t("tform.sortCol")}</Label>
                 <Select
                   value={defaultSortCol || "none"}
                   onValueChange={(v) => setDefaultSortCol(v === "none" ? "" : v)}
                 >
                   <SelectTrigger className="h-9 text-xs">
                     <SelectValue placeholder={t("tform.noneKey")} />
                   </SelectTrigger>
                   <SelectContent>
                     <SelectItem value="none" className="text-xs">{t("tform.noneKey")}</SelectItem>
                    {cols.filter((c) => c.sortable).map((c) => (
                      <SelectItem key={c.name} value={c.name} className="text-xs">
                        {c.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
               <div className="space-y-1.5">
                 <Label className="text-xs font-medium">{t("tform.sortDir")}</Label>
                <Select
                  value={defaultSortDir}
                  onValueChange={(v) => setDefaultSortDir(v === "DESC" ? "DESC" : "ASC")}
                  disabled={!defaultSortCol}
                >
                  <SelectTrigger className="h-9 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                   <SelectContent>
                     <SelectItem value="ASC" className="text-xs">{t("tform.asc")}</SelectItem>
                     <SelectItem value="DESC" className="text-xs">{t("tform.desc")}</SelectItem>
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
              onAddComputed={() =>
                setCols((prev) => [
                  ...prev,
                  { name: `computed_${prev.length + 1}`, label: t("tform.computedCol"),
                    fieldType: "number", enumOptions: null, editable: false, required: false,
                    visible: true, searchable: false, sortable: false, position: 1000 + prev.length,
                    isComputed: true, computedFormula: "" },
                ])
              }
            />

            <M2MRelationsEditor cols={cols} setCols={setCols} defs={defs.data ?? []} currentId={id} />

            <ViewSettingsCard
              cols={cols}
              defaultView={defaultView}
              setDefaultView={setDefaultView}
              viewConfig={viewConfig}
              setViewConfig={setViewConfig}
            />

            <Card className="border-border/60">
              <CardHeader className="pb-3 border-b">
                <CardTitle className="text-sm font-semibold flex items-center gap-1.5">
                  {t("tf.hooks.title")}
                  <HelpPopover title={t("tf.hooks.title")} placement="bottom">
                    <p>{t("tf.hooks.help")}</p>
                  </HelpPopover>
                </CardTitle>
                <CardDescription className="text-xs">{t("tf.hooks.hint")}</CardDescription>
              </CardHeader>
              <CardContent className="pt-4">
                {(hookNames.data?.hooks ?? []).length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("tf.hooks.none")}</p>
                ) : (
                  <HooksEditor
                    value={hooksCfg}
                    names={hookNames.data?.hooks ?? []}
                    onChange={setHooksCfg}
                  />
                )}
              </CardContent>
            </Card>

            {save.isError && (
              <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive">
                {String((save.error as Error).message)}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-4 border-t">
              <Link to="/">
                <Button variant="outline">{t("form.cancel")}</Button>
              </Link>
              <Button
                disabled={!step2Complete || !keysValid || save.isPending}
                onClick={() => save.mutate()}
                className="bg-blue-600 text-white hover:bg-blue-700 gap-1.5"
              >
                <Save className="h-4 w-4" />
                {save.isPending ? t("form.saving") : isEditing ? t("tform.saveChanges") : keysValid ? t("tform.createTitle") : t("tform.needKey")}
              </Button>
            </div>
          </fieldset>
        </CardContent>
      </Card>
    </div>
  );
}

function ViewSettingsCard({ cols, defaultView, setDefaultView, viewConfig, setViewConfig }: {
  cols: FormCol[]; defaultView: "grid" | "kanban" | "calendar";
  setDefaultView: (v: "grid" | "kanban" | "calendar") => void;
  viewConfig: ViewConfig; setViewConfig: (vc: ViewConfig) => void;
}) {
  const t = useT();
  const enums = cols.filter((c) => c.fieldType === "enum" && !c.isComputed);
  const datetimes = cols.filter((c) => c.fieldType === "datetime" && !c.isComputed);
  const visibleCols = cols.filter((c) => c.visible && !c.isComputed);
  return (
    <Card className="border-border/60">
      <CardHeader className="pb-3 border-b">
        <CardTitle className="text-sm font-semibold">{t("view.title")}</CardTitle>
        <CardDescription className="text-xs">{t("view.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="pt-4 space-y-4">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
           {/* Default View Select */}
           <div className="space-y-1.5">
             <div className="flex items-center gap-1.5">
               <Label className="text-xs font-medium">{t("view.default")}</Label>
               <HelpPopover title={t("view.default")} placement="bottom">
                 <p>{t("view.defaultHelp")}</p>
               </HelpPopover>
             </div>
             <Select value={defaultView} onValueChange={(v) => setDefaultView(v as "grid" | "kanban" | "calendar")}>
               <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
               <SelectContent>
                 <SelectItem value="grid" className="text-xs">{t("view.grid")}</SelectItem>
                 <SelectItem value="kanban" className="text-xs">{t("view.kanban")}</SelectItem>
                 <SelectItem value="calendar" className="text-xs">{t("view.calendar")}</SelectItem>
               </SelectContent>
             </Select>
           </div>

           {/* Kanban Settings - Shown ONLY when defaultView === 'kanban' */}
           {defaultView === "kanban" && (
             <>
               <div className="space-y-1.5">
                 <div className="flex items-center gap-1.5">
                   <Label className="text-xs font-medium">{t("view.boardCol")}</Label>
                   <HelpPopover title={t("view.boardCol")} placement="bottom">
                     <p>{t("view.boardColHint")}</p>
                   </HelpPopover>
                 </div>
                 <Select value={viewConfig.kanbanBoardColumn ?? "none"}
                   onValueChange={(v) => setViewConfig({ ...viewConfig, kanbanBoardColumn: v === "none" ? undefined : v })}>
                   <SelectTrigger className="h-9 text-xs"><SelectValue placeholder={t("view.noneKanban")} /></SelectTrigger>
                   <SelectContent>
                     <SelectItem value="none" className="text-xs">{t("view.noneKanban")}</SelectItem>
                     {enums.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs font-mono">{c.label}</SelectItem>)}
                   </SelectContent>
                 </Select>
               </div>

               <div className="space-y-1.5">
                 <div className="flex items-center gap-1.5">
                   <Label className="text-xs font-medium">{t("view.cardTitle")}</Label>
                   <HelpPopover title={t("view.cardTitle")} placement="bottom">
                     <p>{t("view.cardTitleHelp")}</p>
                   </HelpPopover>
                 </div>
                 <Select value={viewConfig.kanbanDisplayColumn ?? "none"}
                   onValueChange={(v) => setViewConfig({ ...viewConfig, kanbanDisplayColumn: v === "none" ? undefined : v })}>
                   <SelectTrigger className="h-9 text-xs"><SelectValue placeholder={t("tform.noneKey")} /></SelectTrigger>
                   <SelectContent>
                     <SelectItem value="none" className="text-xs">{t("tform.noneKey")}</SelectItem>
                     {visibleCols.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs font-mono">{c.label}</SelectItem>)}
                   </SelectContent>
                 </Select>
               </div>
             </>
           )}

           {/* Calendar Settings - Shown ONLY when defaultView === 'calendar' */}
           {defaultView === "calendar" && (
             <>
               <div className="space-y-1.5">
                 <div className="flex items-center gap-1.5">
                   <Label className="text-xs font-medium">{t("view.startCol")}</Label>
                   <HelpPopover title={t("view.startCol")} placement="bottom">
                     <p>{t("view.startColHelp")}</p>
                   </HelpPopover>
                 </div>
                 <Select value={viewConfig.calendarStartColumn ?? "none"}
                   onValueChange={(v) => setViewConfig({ ...viewConfig, calendarStartColumn: v === "none" ? undefined : v })}>
                   <SelectTrigger className="h-9 text-xs"><SelectValue placeholder={t("view.noneCalendar")} /></SelectTrigger>
                   <SelectContent>
                     <SelectItem value="none" className="text-xs">{t("view.noneCalendar")}</SelectItem>
                     {datetimes.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs font-mono">{c.label}</SelectItem>)}
                   </SelectContent>
                 </Select>
               </div>

               <div className="space-y-1.5">
                 <div className="flex items-center gap-1.5">
                   <Label className="text-xs font-medium">{t("view.endCol")}</Label>
                   <HelpPopover title={t("view.endCol")} placement="bottom">
                     <p>{t("view.endColHelp")}</p>
                   </HelpPopover>
                 </div>
                 <Select value={viewConfig.calendarEndColumn ?? "none"}
                   onValueChange={(v) => setViewConfig({ ...viewConfig, calendarEndColumn: v === "none" ? undefined : v })}>
                   <SelectTrigger className="h-9 text-xs"><SelectValue placeholder={t("view.noneEnd")} /></SelectTrigger>
                   <SelectContent>
                     <SelectItem value="none" className="text-xs">{t("view.noneEnd")}</SelectItem>
                     {datetimes.filter((c) => c.name !== viewConfig.calendarStartColumn)
                       .map((c) => <SelectItem key={c.name} value={c.name} className="text-xs font-mono">{c.label}</SelectItem>)}
                   </SelectContent>
                 </Select>
               </div>
             </>
           )}
          </div>
          <p className="text-[10px] text-muted-foreground pt-1 border-t border-border/40">
            {t("view.hint")}
          </p>
       </CardContent>
    </Card>
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
