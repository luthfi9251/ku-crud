import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../lib/api";
import type { Datasource } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const empty = { name: "", host: "localhost", port: 5432, dbname: "", username: "postgres", password: "", sslmode: "disable" };

export default function Datasources() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["ds"], queryFn: () => api<Datasource[]>("/datasources") });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Datasource | null>(null);
  const [form, setForm] = useState({ ...empty });
  const [msg, setMsg] = useState("");

  const save = useMutation({
    mutationFn: () => {
      const body = JSON.stringify(form);
      return editing
        ? api(`/datasources/${editing.id}`, { method: "PUT", body })
        : api("/datasources", { method: "POST", body });
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["ds"] }); setOpen(false); },
    onError: (e) => setMsg(e instanceof ApiError ? `${e.message}: ${String(e.detail ?? "")}` : "failed"),
  });
  const test = useMutation({
    mutationFn: (id: number) => api(`/datasources/${id}/test`, { method: "POST" }),
    onSuccess: () => setMsg("connection ok"),
    onError: (e) => setMsg(e instanceof ApiError ? `${e.message}: ${String(e.detail ?? "")}` : "failed"),
  });

  const set = (k: string, v: string | number) => setForm((f) => ({ ...f, [k]: v }));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Datasources</h1>
        <Button onClick={() => { setEditing(null); setForm({ ...empty }); setMsg(""); setOpen(true); }}>Add datasource</Button>
      </div>
      {msg && <p className="text-sm text-muted-foreground">{msg}</p>}
      <Table>
        <TableHeader><TableRow>
          <TableHead>Name</TableHead><TableHead>Host</TableHead><TableHead>Database</TableHead>
          <TableHead>User</TableHead><TableHead>SSL</TableHead><TableHead></TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {(list.data ?? []).map((d) => (
            <TableRow key={d.id}>
              <TableCell>{d.name}</TableCell>
              <TableCell>{d.host}:{d.port}</TableCell>
              <TableCell>{d.dbname}</TableCell>
              <TableCell>{d.username}</TableCell>
              <TableCell>{d.sslmode}</TableCell>
              <TableCell className="space-x-2 text-right">
                <Button variant="outline" size="sm" onClick={() => test.mutate(d.id)}>Test</Button>
                <Button variant="outline" size="sm" onClick={() => {
                  setEditing(d);
                  setForm({ name: d.name, host: d.host, port: d.port, dbname: d.dbname, username: d.username, password: "", sslmode: d.sslmode });
                  setMsg(""); setOpen(true);
                }}>Edit</Button>
                <Button variant="outline" size="sm" onClick={async () => {
                  if (!confirm(`Delete datasource ${d.name}?`)) return;
                  await api(`/datasources/${d.id}`, { method: "DELETE" });
                  qc.invalidateQueries({ queryKey: ["ds"] });
                }}>Delete</Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>{editing ? "Edit datasource" : "New datasource"}</DialogTitle></DialogHeader>
          <div className="grid gap-3">
            {([["name", "Name"], ["host", "Host"], ["dbname", "Database (required)"], ["username", "Username"], ["password", editing ? "Password (blank = unchanged)" : "Password"]] as const).map(([k, label]) => (
              <div key={k} className="space-y-1">
                <Label>{label}</Label>
                <Input value={form[k]} onChange={(e) => set(k, e.target.value)} />
              </div>
            ))}
            <div className="space-y-1">
              <Label>Port</Label>
              <Input type="number" value={form.port} onChange={(e) => set("port", Number(e.target.value))} />
            </div>
            <div className="space-y-1">
              <Label>SSL mode</Label>
              <Select value={form.sslmode} onValueChange={(v) => set("sslmode", v)}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {["disable", "require", "verify-ca", "verify-full"].map((m) => <SelectItem key={m} value={m}>{m}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => save.mutate()} disabled={save.isPending}>{editing ? "Save" : "Create"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
