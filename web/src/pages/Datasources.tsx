import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Server, Plus, Edit, Trash2, Zap, CheckCircle2, AlertCircle, ShieldCheck, Database, HardDrive } from "lucide-react";
import { api, ApiError } from "../lib/api";
import type { Datasource } from "../lib/types";
import { useT } from "../lib/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

const empty = { name: "", driver: "postgres", host: "localhost", port: 5432, dbname: "", username: "postgres", password: "", sslmode: "disable" };
const defaultPorts: Record<string, number> = { postgres: 5432, mysql: 3306 };
const driverLabels: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL" };

export default function Datasources() {
  const qc = useQueryClient();
  const t = useT();
  const list = useQuery({ queryKey: ["ds"], queryFn: () => api<Datasource[]>("/datasources") });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Datasource | null>(null);
  const [form, setForm] = useState({ ...empty });
  const [msg, setMsg] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [testingId, setTestingId] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: () => {
      const body = JSON.stringify(form);
      return editing
        ? api(`/datasources/${editing.id}`, { method: "PUT", body })
        : api("/datasources", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["ds"] });
      setOpen(false);
      setMsg({ type: "success", text: editing ? t("ds.updated") : t("ds.created") });
    },
    onError: (e) => setMsg({ type: "error", text: e instanceof ApiError ? `${e.message}: ${String(e.detail ?? "")}` : t("ds.saveFailed") }),
  });

  const test = useMutation({
    mutationFn: (id: string) => {
      setTestingId(id);
      return api(`/datasources/${id}/test`, { method: "POST" });
    },
    onSuccess: () => {
      setTestingId(null);
      setMsg({ type: "success", text: t("ds.testOk") });
    },
    onError: (e) => {
      setTestingId(null);
      setMsg({ type: "error", text: e instanceof ApiError ? `${e.message}: ${String(e.detail ?? "")}` : t("ds.testFailed") });
    },
  });

  const set = (k: string, v: string | number) => setForm((f) => ({ ...f, [k]: v }));

  return (
    <div className="space-y-6">
      {/* Top Action Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between border-b pb-4">
        <div>
          <h2 className="text-xl font-bold tracking-tight">{t("ds.title")}</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t("ds.subtitle")}
          </p>
        </div>
        <Button
          onClick={() => {
            setEditing(null);
            setForm({ ...empty });
            setMsg(null);
            setOpen(true);
          }}
          className="bg-blue-600 text-white hover:bg-blue-700 shadow-xs gap-1.5"
        >
          <Plus className="h-4 w-4" /> {t("ds.add")}
        </Button>
      </div>

      {/* Alert Status Banner */}
      {msg && (
        <div
          className={`flex items-center gap-2.5 rounded-lg border p-3.5 text-xs font-medium transition-all ${
            msg.type === "success"
              ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
              : "border-destructive/30 bg-destructive/10 text-destructive"
          }`}
        >
          {msg.type === "success" ? <CheckCircle2 className="h-4 w-4 shrink-0" /> : <AlertCircle className="h-4 w-4 shrink-0" />}
          <span>{msg.text}</span>
        </div>
      )}

      {/* Grid of Datasource Cards */}
      {list.isLoading ? (
        <div className="text-center py-12 text-xs text-muted-foreground">{t("ds.loading")}</div>
      ) : (list.data ?? []).length === 0 ? (
        <Card className="border-dashed p-12 text-center">
          <div className="flex flex-col items-center justify-center space-y-3">
            <Server className="h-10 w-10 text-muted-foreground/40" />
            <div>
              <p className="text-base font-semibold">{t("ds.empty")}</p>
              <p className="text-xs text-muted-foreground mt-1">
                {t("ds.emptyHint")}
              </p>
            </div>
            <Button
              onClick={() => {
                setEditing(null);
                setForm({ ...empty });
                setMsg(null);
                setOpen(true);
              }}
              className="bg-blue-600 text-white hover:bg-blue-700 text-xs mt-2"
            >
              <Plus className="h-3.5 w-3.5 mr-1" /> {t("ds.create")}
            </Button>
          </div>
        </Card>
      ) : (
        <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
          {(list.data ?? []).map((d) => (
            <Card key={d.id} className="relative overflow-hidden border-border/60 hover:shadow-md transition-all group">
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2.5">
                    <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
                      <Database className="h-4 w-4" />
                    </div>
                    <div>
                      <CardTitle className="text-base font-bold">{d.name}</CardTitle>
                      <CardDescription className="text-[11px] font-mono mt-0.5">{d.dbname}</CardDescription>
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <Badge variant="outline" className="text-[10px] font-normal bg-muted/50">
                      {driverLabels[d.driver] ?? d.driver}
                    </Badge>
                    <Badge variant="outline" className="text-[10px] font-normal gap-1 bg-muted/50">
                      <ShieldCheck className="h-3 w-3 text-emerald-500" /> {t("ds.ssl", { mode: d.sslmode })}
                    </Badge>
                  </div>
                </div>
              </CardHeader>

              <CardContent className="space-y-4 text-xs">
                <div className="grid grid-cols-2 gap-2 rounded-lg bg-muted/40 p-3 font-mono">
                  <div>
                    <span className="text-[10px] text-muted-foreground block">HOST</span>
                    <span className="truncate block font-medium">{d.host}:{d.port}</span>
                  </div>
                  <div>
                    <span className="text-[10px] text-muted-foreground block">USER</span>
                    <span className="truncate block font-medium">{d.username}</span>
                  </div>
                </div>

                <div className="flex items-center justify-between pt-1 border-t border-border/40">
                  <Button
                    variant="secondary"
                    size="sm"
                    className="h-8 gap-1 text-xs"
                    disabled={testingId === d.id}
                    onClick={() => test.mutate(d.id)}
                  >
                    <Zap className={`h-3.5 w-3.5 text-amber-500 ${testingId === d.id ? "animate-spin" : ""}`} />
                    {testingId === d.id ? t("ds.testing") : t("ds.test")}
                  </Button>

                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground hover:text-foreground"
                      onClick={() => {
                        setEditing(d);
                        setForm({
                          name: d.name,
                          driver: d.driver ?? "postgres",
                          host: d.host,
                          port: d.port,
                          dbname: d.dbname,
                          username: d.username,
                          password: "",
                          sslmode: d.sslmode,
                        });
                        setMsg(null);
                        setOpen(true);
                      }}
                      title={t("ds.editTitle")}
                    >
                      <Edit className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground hover:text-destructive"
                      onClick={async () => {
                        if (!confirm(t("ds.deleteConfirm", { name: d.name }))) return;
                        await api(`/datasources/${d.id}`, { method: "DELETE" });
                        qc.invalidateQueries({ queryKey: ["ds"] });
                      }}
                      title={t("ds.deleteTitle")}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* Dialog Modal for New/Edit Datasource */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <HardDrive className="h-5 w-5 text-blue-500" />
              {editing ? t("ds.editDialog") : t("ds.newDialog")}
            </DialogTitle>
            <DialogDescription className="text-xs">
              {t("ds.dialogDesc", { driver: driverLabels[form.driver] ?? form.driver })}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-3 py-2">
            <div className="space-y-1">
              <Label className="text-xs">{t("ds.label")}</Label>
              <Input
                placeholder={t("ds.labelPh")}
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                className="h-9 text-xs"
              />
            </div>

            <div className="space-y-1">
              <Label className="text-xs">{t("ds.driver")}</Label>
              <Select
                value={form.driver}
                onValueChange={(v) => setForm((f) => ({ ...f, driver: v, port: defaultPorts[v] ?? f.port }))}
              >
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(driverLabels).map(([v, label]) => (
                    <SelectItem key={v} value={v} className="text-xs">{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid grid-cols-3 gap-2">
              <div className="col-span-2 space-y-1">
                <Label className="text-xs">{t("ds.host")}</Label>
                <Input
                  placeholder="localhost"
                  value={form.host}
                  onChange={(e) => set("host", e.target.value)}
                  className="h-9 text-xs"
                />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">{t("ds.port")}</Label>
                <Input
                  type="number"
                  value={form.port}
                  onChange={(e) => set("port", Number(e.target.value))}
                  className="h-9 text-xs"
                />
              </div>
            </div>

            <div className="space-y-1">
              <Label className="text-xs">{t("ds.dbname")}</Label>
              <Input
                placeholder="database_name"
                value={form.dbname}
                onChange={(e) => set("dbname", e.target.value)}
                className="h-9 text-xs"
              />
            </div>

            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-1">
                <Label className="text-xs">{t("ds.username")}</Label>
                <Input
                  placeholder="postgres"
                  value={form.username}
                  onChange={(e) => set("username", e.target.value)}
                  className="h-9 text-xs"
                />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">
                  {editing ? t("ds.passwordKeep") : t("ds.password")}
                </Label>
                <Input
                  type="password"
                  placeholder="••••••••"
                  value={form.password}
                  onChange={(e) => set("password", e.target.value)}
                  className="h-9 text-xs"
                />
              </div>
            </div>

            <div className="space-y-1">
              <Label className="text-xs">{t("ds.sslmode")}</Label>
              <Select value={form.sslmode} onValueChange={(v) => set("sslmode", v)}>
                <SelectTrigger className="h-9 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["disable", "require", "verify-ca", "verify-full"].map((m) => (
                    <SelectItem key={m} value={m} className="text-xs">
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button variant="outline" onClick={() => setOpen(false)}>
              {t("form.cancel")}
            </Button>
            <Button
              onClick={() => save.mutate()}
              disabled={save.isPending}
              className="bg-blue-600 text-white hover:bg-blue-700"
            >
              {save.isPending ? t("form.saving") : editing ? t("tform.saveChanges") : t("ds.connect")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
