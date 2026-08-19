import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Table2,
  Plus,
  Trash2,
  Edit,
  ExternalLink,
  Server,
  Layers,
  CheckCircle2,
  Sparkles,
  ArrowRight,
  ArrowLeft,
  Database,
} from "lucide-react";
import { api } from "../lib/api";
import type { ColumnDef, Datasource, LiveColumn, TableDef, TableDefPayload } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const fieldTypes = ["boolean", "text", "number", "datetime", "enum"] as const;

export default function Tables() {
  const qc = useQueryClient();
  const defs = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const dsList = useQuery({ queryKey: ["ds"], queryFn: () => api<Datasource[]>("/datasources") });
  const [wizard, setWizard] = useState<WizardState | null>(null);
  const [search, setSearch] = useState("");

  const filteredDefs = (defs.data ?? []).filter((d) =>
    d.label.toLowerCase().includes(search.toLowerCase()) ||
    d.tableName.toLowerCase().includes(search.toLowerCase()) ||
    d.schemaName.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Header & Overview Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card className="border-border/60 bg-card/60 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">Configured Tables</CardTitle>
            <Table2 className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{defs.data?.length ?? 0}</div>
            <p className="text-[11px] text-muted-foreground mt-1">Managed database schemas</p>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/60 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">Active Datasources</CardTitle>
            <Server className="h-4 w-4 text-indigo-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{dsList.data?.length ?? 0}</div>
            <p className="text-[11px] text-muted-foreground mt-1">Connected database pools</p>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/60 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">Status</CardTitle>
            <Sparkles className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <span className="relative flex h-2.5 w-2.5">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-emerald-500"></span>
              </span>
              <span className="text-sm font-semibold text-emerald-600 dark:text-emerald-400">Ready</span>
            </div>
            <p className="text-[11px] text-muted-foreground mt-1">Auto CRUD API active</p>
          </CardContent>
        </Card>
      </div>

      {/* Main Table List Card */}
      <Card className="border-border/60 shadow-sm">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle className="text-base font-semibold">Table Definitions</CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              Select or register a database table to start managing records
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Input
              placeholder="Filter tables..."
              className="h-9 w-48 text-xs md:w-64"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <Button
              onClick={() =>
                setWizard({
                  step: 1,
                  dsId: "",
                  schema: "",
                  table: "",
                  label: "",
                  pageSize: 20,
                  pk: "",
                  cols: [],
                  editingId: null,
                })
              }
              className="bg-blue-600 text-white hover:bg-blue-700 shadow-xs"
            >
              <Plus className="h-4 w-4 mr-1.5" /> Add Table
            </Button>
          </div>
        </CardHeader>

        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-[30%]">Display Label</TableHead>
                <TableHead className="w-[30%]">Database Table</TableHead>
                <TableHead className="w-[20%]">Primary Key</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {defs.isLoading ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center text-xs text-muted-foreground">
                    Loading table definitions...
                  </TableCell>
                </TableRow>
              ) : filteredDefs.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-32 text-center">
                    <div className="flex flex-col items-center justify-center space-y-2">
                      <Table2 className="h-8 w-8 text-muted-foreground/40" />
                      <p className="text-sm font-medium text-muted-foreground">No table definitions found</p>
                      <p className="text-xs text-muted-foreground/70">
                        Click "Add Table" to map a database table into Ku-CRUD.
                      </p>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                filteredDefs.map((d) => (
                  <TableRow key={d.id} className="hover:bg-muted/30">
                    <TableCell className="font-medium">
                      <Link
                        to={`/data/${d.id}`}
                        className="inline-flex items-center gap-1.5 text-blue-600 dark:text-blue-400 hover:underline font-semibold"
                      >
                        <Layers className="h-3.5 w-3.5" />
                        {d.label}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-muted-foreground bg-muted/60 px-2 py-1 rounded">
                        {d.schemaName}.{d.tableName}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary" className="font-mono text-[11px] font-normal">
                        {d.pkColumn}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Link to={`/data/${d.id}`}>
                          <Button variant="secondary" size="sm" className="h-8 gap-1 text-xs">
                            <ExternalLink className="h-3.5 w-3.5" /> View Data
                          </Button>
                        </Link>
                        <EditButton id={d.id} onOpen={setWizard} />
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
                          onClick={async () => {
                            if (!confirm(`Delete definition "${d.label}"? (Data table itself remains intact)`)) return;
                            await api(`/tables/${d.id}`, { method: "DELETE" });
                            qc.invalidateQueries({ queryKey: ["defs"] });
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {wizard && <Wizard state={wizard} onClose={() => setWizard(null)} />}
    </div>
  );
}

function EditButton({ id, onOpen }: { id: number; onOpen: (w: WizardState) => void }) {
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  return (
    <Button
      variant="outline"
      size="sm"
      className="h-8 gap-1 text-xs"
      disabled={!def.data}
      onClick={() => {
        const d = def.data!;
        onOpen({
          step: 3,
          dsId: String(d.datasourceId),
          schema: d.schemaName,
          table: d.tableName,
          label: d.label,
          pageSize: d.pageSize,
          pk: d.pkColumn,
          cols: d.columns,
          editingId: d.id,
        });
      }}
    >
      <Edit className="h-3.5 w-3.5" /> Edit
    </Button>
  );
}

interface WizardState {
  step: 1 | 2 | 3;
  dsId: string;
  schema: string;
  table: string;
  label: string;
  pageSize: number;
  pk: string;
  cols: WizardCol[];
  editingId: number | null;
}

interface WizardCol extends ColumnDef {
  livePk?: boolean;
}

function Wizard({ state, onClose }: { state: WizardState; onClose: () => void }) {
  const qc = useQueryClient();
  const [w, setW] = useState(state);
  const dsList = useQuery({ queryKey: ["ds"], queryFn: () => api<Datasource[]>("/datasources") });
  const tables = useQuery({
    queryKey: ["ds-tables", w.dsId],
    enabled: w.step >= 2 && w.dsId !== "",
    queryFn: () => api<{ schema: string; name: string }[]>(`/datasources/${w.dsId}/tables`),
  });
  const live = useQuery({
    queryKey: ["ds-cols", w.dsId, w.schema, w.table],
    enabled: w.step === 3 && !w.editingId,
    queryFn: () => api<LiveColumn[]>(`/datasources/${w.dsId}/tables/${w.schema}/${w.table}/columns`),
  });

  if (w.step === 3 && !w.editingId && live.data && w.cols.length === 0 && live.data.length > 0) {
    setW((s) => ({
      ...s,
      label: s.label || (s.table.charAt(0).toUpperCase() + s.table.slice(1)),
      pk: s.pk || live.data.find((c) => c.isPk)?.name || "",
      cols: live.data.map((c, i) => ({
        name: c.name,
        label: c.name,
        fieldType: c.fieldType,
        enumOptions: c.enumOptions,
        editable: !c.isPk,
        required: !c.nullable,
        visible: true,
        searchable: true,
        sortable: true,
        position: i,
        livePk: c.isPk,
      })),
    }));
  }

  const save = useMutation({
    mutationFn: () => {
      const body = JSON.stringify({
        datasourceId: Number(w.dsId),
        schemaName: w.schema,
        tableName: w.table,
        label: w.label,
        pkColumn: w.pk,
        pageSize: w.pageSize,
        columns: w.cols.map(({ livePk: _lp, ...c }) => c),
      });
      return w.editingId
        ? api(`/tables/${w.editingId}`, { method: "PUT", body })
        : api("/tables", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["defs"] });
      onClose();
    },
  });

  const setCol = (i: number, patch: Partial<WizardCol>) =>
    setW((s) => ({ ...s, cols: s.cols.map((c, j) => (j === i ? { ...c, ...patch } : c)) }));
  const pkValid = w.pk && w.cols.some((c) => c.name === w.pk);

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[95vw] md:max-w-6xl max-h-[92vh] overflow-y-auto">
        <DialogHeader className="border-b pb-4">
          <DialogTitle className="text-lg font-semibold flex items-center gap-2">
            <Database className="h-5 w-5 text-blue-500" />
            {w.editingId ? `Edit Definition: ${w.label}` : "Table Definition Wizard"}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {!w.editingId ? "Map database table columns and configure CRUD form behavior" : `Definition ID #${w.editingId}`}
          </DialogDescription>

          {/* Stepper Progress */}
          {!w.editingId && (
            <div className="flex items-center justify-between pt-3 max-w-md">
              {[
                { step: 1, label: "1. Datasource" },
                { step: 2, label: "2. Select Table" },
                { step: 3, label: "3. Configure Columns" },
              ].map((s) => (
                <div key={s.step} className="flex items-center gap-1.5">
                  <span
                    className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold ${
                      w.step === s.step
                        ? "bg-blue-600 text-white"
                        : w.step > s.step
                        ? "bg-emerald-500 text-white"
                        : "bg-muted text-muted-foreground"
                    }`}
                  >
                    {w.step > s.step ? <CheckCircle2 className="h-4 w-4" /> : s.step}
                  </span>
                  <span className={`text-xs ${w.step === s.step ? "font-semibold text-foreground" : "text-muted-foreground"}`}>
                    {s.label}
                  </span>
                  {s.step < 3 && <ArrowRight className="h-3 w-3 text-muted-foreground/40 ml-1" />}
                </div>
              ))}
            </div>
          )}
        </DialogHeader>

        {/* Step 1: Pick Datasource */}
        {w.step === 1 && (
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label className="text-xs font-semibold">Select Datasource Connection</Label>
              <Select value={w.dsId} onValueChange={(v) => setW({ ...w, dsId: v, step: 2 })}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Choose a database connection..." />
                </SelectTrigger>
                <SelectContent>
                  {(dsList.data ?? []).map((d) => (
                    <SelectItem key={d.id} value={String(d.id)}>
                      <div className="flex items-center gap-2">
                        <Server className="h-4 w-4 text-blue-500" />
                        <span className="font-medium">{d.name}</span>
                        <span className="text-xs text-muted-foreground">({d.dbname})</span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {(dsList.data ?? []).length === 0 && (
              <p className="text-xs text-amber-600 dark:text-amber-400 bg-amber-500/10 p-3 rounded-lg border border-amber-500/20">
                No active datasources found. Please configure a Datasource first.
              </p>
            )}
          </div>
        )}

        {/* Step 2: Pick Table */}
        {w.step === 2 && (
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label className="text-xs font-semibold">Select Table from Database Schema</Label>
              <Select
                value={`${w.schema}.${w.table}`}
                onValueChange={(v) => {
                  const [schema, table] = v.split(".");
                  setW({ ...w, schema, table, step: 3 });
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Choose a database table..." />
                </SelectTrigger>
                <SelectContent>
                  {(tables.data ?? []).map((t) => (
                    <SelectItem key={`${t.schema}.${t.name}`} value={`${t.schema}.${t.name}`}>
                      <span className="font-mono text-xs">{t.schema}.{t.name}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex justify-start pt-2">
              <Button
                variant="outline"
                size="sm"
                className="text-xs gap-1"
                onClick={() => setW({ ...w, step: 1 })}
              >
                <ArrowLeft className="h-3.5 w-3.5" /> Back to Datasources
              </Button>
            </div>
          </div>
        )}

        {/* Step 3: Column Mapping & Configuration */}
        {w.step === 3 && (
          <div className="space-y-4 py-2">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1">
                <Label className="text-xs">Display Label</Label>
                <Input value={w.label} onChange={(e) => setW({ ...w, label: e.target.value })} placeholder="e.g. User Accounts" />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Page Size</Label>
                <Input type="number" value={w.pageSize} onChange={(e) => setW({ ...w, pageSize: Number(e.target.value) })} />
              </div>
            </div>

            <div className="rounded-lg border bg-card overflow-hidden">
              <div className="max-h-72 overflow-y-auto">
                <Table>
                  <TableHeader>
                    <TableRow className="bg-muted/50">
                      <TableHead className="w-10">PK</TableHead>
                      <TableHead>Column</TableHead>
                      <TableHead className="w-40">Label</TableHead>
                      <TableHead className="w-32">Type</TableHead>
                      <TableHead className="text-center">Editable</TableHead>
                      <TableHead className="text-center">Required</TableHead>
                      <TableHead className="text-center">Visible</TableHead>
                      <TableHead className="text-center">Search</TableHead>
                      <TableHead className="text-center">Sort</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {w.cols.map((c, i) => (
                      <TableRow key={c.name} className="hover:bg-muted/20">
                        <TableCell>
                          <input
                            type="radio"
                            name="pk"
                            className="h-4 w-4 accent-blue-600 cursor-pointer"
                            checked={w.pk === c.name}
                            onChange={() => setW({ ...w, pk: c.name })}
                          />
                        </TableCell>
                        <TableCell className="font-mono text-xs font-medium">{c.name}</TableCell>
                        <TableCell>
                          <Input
                            value={c.label}
                            className="h-8 text-xs"
                            onChange={(e) => setCol(i, { label: e.target.value })}
                          />
                        </TableCell>
                        <TableCell>
                          <Select
                            value={c.fieldType}
                            onValueChange={(v) =>
                              setCol(i, {
                                fieldType: v as ColumnDef["fieldType"],
                                enumOptions: v === "enum" ? (c.enumOptions ?? []) : null,
                              })
                            }
                          >
                            <SelectTrigger className="h-8 text-xs">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {fieldTypes.map((t) => (
                                <SelectItem key={t} value={t} className="text-xs">
                                  {t}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </TableCell>
                        <TableCell className="text-center">
                          <Switch checked={c.editable} onCheckedChange={(v) => setCol(i, { editable: v })} />
                        </TableCell>
                        <TableCell className="text-center">
                          <Switch checked={c.required} onCheckedChange={(v) => setCol(i, { required: v })} />
                        </TableCell>
                        <TableCell className="text-center">
                          <Switch checked={c.visible} onCheckedChange={(v) => setCol(i, { visible: v })} />
                        </TableCell>
                        <TableCell className="text-center">
                          <Switch checked={c.searchable} onCheckedChange={(v) => setCol(i, { searchable: v })} />
                        </TableCell>
                        <TableCell className="text-center">
                          <Switch checked={c.sortable} onCheckedChange={(v) => setCol(i, { sortable: v })} />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>

            {w.cols.some((c) => c.fieldType === "enum") && <EnumOptions cols={w.cols} setCol={setCol} />}

            {save.isError && (
              <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive">
                {String((save.error as Error).message)}
              </div>
            )}

            <div className="flex items-center justify-between pt-2">
              <div>
                {!w.editingId && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-xs gap-1"
                    onClick={() => setW({ ...w, step: 2 })}
                  >
                    <ArrowLeft className="h-3.5 w-3.5" /> Back to Select Table
                  </Button>
                )}
              </div>
              <div className="flex gap-2">
                <Button variant="outline" onClick={onClose}>
                  Cancel
                </Button>
                <Button
                  disabled={!pkValid || save.isPending}
                  onClick={() => save.mutate()}
                  className="bg-blue-600 text-white hover:bg-blue-700"
                >
                  {save.isPending ? "Saving..." : pkValid ? "Save Definition" : "Select Primary Key"}
                </Button>
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function EnumOptions({ cols, setCol }: { cols: WizardCol[]; setCol: (i: number, p: Partial<WizardCol>) => void }) {
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <p className="text-xs font-semibold text-foreground">Enum Options</p>
      {cols
        .filter((c) => c.fieldType === "enum")
        .map((c) => {
          const i = cols.indexOf(c);
          return (
            <div key={c.name} className="space-y-1">
              <Label className="text-xs text-muted-foreground">{c.label} options (comma separated)</Label>
              <Input
                className="h-8 text-xs"
                value={(c.enumOptions ?? []).join(",")}
                onChange={(e) =>
                  setCol(i, { enumOptions: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })
                }
              />
            </div>
          );
        })}
    </div>
  );
}
