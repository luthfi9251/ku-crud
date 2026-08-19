import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { ColumnDef, Datasource, LiveColumn, TableDef, TableDefPayload } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const fieldTypes = ["boolean", "text", "number", "datetime", "enum"] as const;

export default function Tables() {
  const qc = useQueryClient();
  const defs = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const [wizard, setWizard] = useState<WizardState | null>(null);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Tables</h1>
        <Button onClick={() => setWizard({ step: 1, dsId: "", schema: "", table: "", label: "", pageSize: 20,
          pk: "", cols: [], editingId: null })}>Add table</Button>
      </div>
      <Table>
        <TableHeader><TableRow>
          <TableHead>Label</TableHead><TableHead>Table</TableHead><TableHead>PK</TableHead><TableHead></TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {(defs.data ?? []).map((d) => (
            <TableRow key={d.id}>
              <TableCell>
                <a className="font-medium underline" href={`#/data/${d.id}`}>{d.label}</a>
              </TableCell>
              <TableCell className="text-muted-foreground">{d.schemaName}.{d.tableName}</TableCell>
              <TableCell><Badge variant="secondary">{d.pkColumn}</Badge></TableCell>
              <TableCell className="space-x-2 text-right">
                <EditButton id={d.id} onOpen={setWizard} />
                <Button variant="outline" size="sm" onClick={async () => {
                  if (!confirm(`Delete definition ${d.label}? (data is untouched)`)) return;
                  await api(`/tables/${d.id}`, { method: "DELETE" });
                  qc.invalidateQueries({ queryKey: ["defs"] });
                }}>Delete</Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {wizard && <Wizard state={wizard} onClose={() => setWizard(null)} />}
    </div>
  );
}

function EditButton({ id, onOpen }: { id: number; onOpen: (w: WizardState) => void }) {
  const def = useQuery({ queryKey: ["def", id], queryFn: () => api<TableDefPayload>(`/tables/${id}`) });
  return (
    <Button variant="outline" size="sm" disabled={!def.data} onClick={() => {
      const d = def.data!;
      onOpen({ step: 3, dsId: String(d.datasourceId), schema: d.schemaName, table: d.tableName,
        label: d.label, pageSize: d.pageSize, pk: d.pkColumn, cols: d.columns, editingId: d.id });
    }}>Edit</Button>
  );
}

interface WizardState {
  step: 1 | 2 | 3;
  dsId: string; schema: string; table: string;
  label: string; pageSize: number; pk: string;
  cols: WizardCol[];
  editingId: number | null;
}

interface WizardCol extends ColumnDef { livePk?: boolean }

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
        name: c.name, label: c.name, fieldType: c.fieldType, enumOptions: c.enumOptions,
        editable: !c.isPk, required: !c.nullable, visible: true,
        searchable: true, sortable: true, position: i, livePk: c.isPk,
      })),
    }));
  }

  const save = useMutation({
    mutationFn: () => {
      const body = JSON.stringify({
        datasourceId: Number(w.dsId), schemaName: w.schema, tableName: w.table,
        label: w.label, pkColumn: w.pk, pageSize: w.pageSize,
        columns: w.cols.map(({ livePk: _lp, ...c }) => c),
      });
      return w.editingId
        ? api(`/tables/${w.editingId}`, { method: "PUT", body })
        : api("/tables", { method: "POST", body });
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["defs"] }); onClose(); },
  });

  const setCol = (i: number, patch: Partial<WizardCol>) =>
    setW((s) => ({ ...s, cols: s.cols.map((c, j) => (j === i ? { ...c, ...patch } : c)) }));
  const pkValid = w.pk && w.cols.some((c) => c.name === w.pk);

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Define table {w.editingId ? `(editing #${w.editingId})` : `— step ${w.step}/3`}</DialogTitle>
        </DialogHeader>

        {w.step === 1 && (
          <div className="space-y-3">
            <Label>Datasource</Label>
            <Select value={w.dsId} onValueChange={(v) => setW({ ...w, dsId: v, step: 2 })}>
              <SelectTrigger><SelectValue placeholder="pick a datasource" /></SelectTrigger>
              <SelectContent>
                {(dsList.data ?? []).map((d) => (
                  <SelectItem key={d.id} value={String(d.id)}>{d.name} ({d.dbname})</SelectItem>
                ))}
              </SelectContent>
            </Select>
            {(dsList.data ?? []).length === 0 && <p className="text-sm text-muted-foreground">Create a datasource first.</p>}
          </div>
        )}

        {w.step === 2 && (
          <div className="space-y-3">
            <Label>Table</Label>
            <Select value={`${w.schema}.${w.table}`} onValueChange={(v) => {
              const [schema, table] = v.split(".");
              setW({ ...w, schema, table, step: 3 });
            }}>
              <SelectTrigger><SelectValue placeholder="pick a table" /></SelectTrigger>
              <SelectContent>
                {(tables.data ?? []).map((t) => (
                  <SelectItem key={`${t.schema}.${t.name}`} value={`${t.schema}.${t.name}`}>{t.schema}.{t.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {w.step === 3 && (
          <div className="space-y-4">
            <div className="flex gap-3">
              <div className="flex-1 space-y-1">
                <Label>Label</Label>
                <Input value={w.label} onChange={(e) => setW({ ...w, label: e.target.value })} />
              </div>
              <div className="w-32 space-y-1">
                <Label>Page size</Label>
                <Input type="number" value={w.pageSize} onChange={(e) => setW({ ...w, pageSize: Number(e.target.value) })} />
              </div>
            </div>
            <div className="max-h-80 overflow-y-auto rounded border">
              <Table>
                <TableHeader><TableRow>
                  <TableHead>PK</TableHead><TableHead>Column</TableHead><TableHead>Label</TableHead>
                  <TableHead>Type</TableHead><TableHead>Edit</TableHead><TableHead>Req</TableHead>
                  <TableHead>Visible</TableHead><TableHead>Search</TableHead><TableHead>Sort</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {w.cols.map((c, i) => (
                    <TableRow key={c.name}>
                      <TableCell>
                        <input type="radio" name="pk" checked={w.pk === c.name} onChange={() => setW({ ...w, pk: c.name })} />
                      </TableCell>
                      <TableCell className="font-mono text-xs">{c.name}</TableCell>
                      <TableCell><Input value={c.label} onChange={(e) => setCol(i, { label: e.target.value })} /></TableCell>
                      <TableCell>
                        <Select value={c.fieldType} onValueChange={(v) => setCol(i, {
                          fieldType: v as ColumnDef["fieldType"],
                          enumOptions: v === "enum" ? (c.enumOptions ?? []) : null,
                        })}>
                          <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            {fieldTypes.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell><Switch checked={c.editable} onCheckedChange={(v) => setCol(i, { editable: v })} /></TableCell>
                      <TableCell><Switch checked={c.required} onCheckedChange={(v) => setCol(i, { required: v })} /></TableCell>
                      <TableCell><Switch checked={c.visible} onCheckedChange={(v) => setCol(i, { visible: v })} /></TableCell>
                      <TableCell><Switch checked={c.searchable} onCheckedChange={(v) => setCol(i, { searchable: v })} /></TableCell>
                      <TableCell><Switch checked={c.sortable} onCheckedChange={(v) => setCol(i, { sortable: v })} /></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {w.cols.some((c) => c.fieldType === "enum") && (
              <EnumOptions cols={w.cols} setCol={setCol} />
            )}
            {save.isError && <p className="text-sm text-destructive">{String((save.error as Error).message)}</p>}
            <Button disabled={!pkValid || save.isPending} onClick={() => save.mutate()}>
              {pkValid ? "Save definition" : "pick a PK column"}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function EnumOptions({ cols, setCol }: { cols: WizardCol[]; setCol: (i: number, p: Partial<WizardCol>) => void }) {
  return (
    <div className="space-y-2">
      {cols.filter((c) => c.fieldType === "enum").map((c) => {
        const i = cols.indexOf(c);
        return (
          <div key={c.name} className="space-y-1">
            <Label>{c.label} options (comma separated)</Label>
            <Input value={(c.enumOptions ?? []).join(",")}
              onChange={(e) => setCol(i, { enumOptions: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} />
          </div>
        );
      })}
    </div>
  );
}
