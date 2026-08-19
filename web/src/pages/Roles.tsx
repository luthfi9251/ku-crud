import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, Plus, Trash2, Pencil, Users as UsersIcon } from "lucide-react";
import { api } from "../lib/api";
import type { Role, TableDef, TableGrant } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface RoleForm {
  mode: "new" | "edit";
  id: string;
  name: string;
  platformManage: boolean;
  grants: Record<string, TableGrant>;
}

const emptyForm: RoleForm = { mode: "new", id: "", name: "", platformManage: false, grants: {} };

export default function Roles() {
  const qc = useQueryClient();
  const roles = useQuery({ queryKey: ["roles"], queryFn: () => api<Role[]>("/roles") });
  const defs = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const [form, setForm] = useState<RoleForm | null>(null);

  const grantFor = (f: RoleForm, defId: string): TableGrant =>
    f.grants[defId] ?? { tableDefId: defId, canRead: false, canCreate: false, canUpdate: false, canDelete: false };

  const setGrant = (f: RoleForm, defId: string, patch: Partial<TableGrant>): RoleForm => ({
    ...f,
    grants: { ...f.grants, [defId]: { ...grantFor(f, defId), ...patch } },
  });

  const openEdit = (r: Role) => {
    const grants: Record<string, TableGrant> = {};
    r.tables.forEach((g) => (grants[g.tableDefId] = g));
    setForm({ mode: "edit", id: r.id, name: r.name, platformManage: r.platformManage, grants });
  };

  const save = useMutation({
    mutationFn: () => {
      const tables = (defs.data ?? []).map((d) => grantFor(form!, d.id)).filter((g) => g.canRead || g.canCreate || g.canUpdate || g.canDelete);
      const body = JSON.stringify({ name: form!.name, platformManage: form!.platformManage, tables });
      return form!.mode === "new"
        ? api("/roles", { method: "POST", body })
        : api(`/roles/${form!.id}`, { method: "PUT", body });
    },
    onSuccess: () => {
      setForm(null);
      qc.invalidateQueries({ queryKey: ["roles"] });
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => api(`/roles/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["roles"] }),
  });

  return (
    <div className="space-y-6">
      <Card className="border-border/60 shadow-sm">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <ShieldCheck className="h-4 w-4 text-indigo-500" /> Roles
            </CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              Define access profiles: platform management bundle plus per-table CRUD grants
            </p>
          </div>
          <Button onClick={() => setForm({ ...emptyForm })} className="bg-blue-600 text-white hover:bg-blue-700 shadow-xs">
            <Plus className="h-4 w-4 mr-1.5" /> Add Role
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-[25%]">Role</TableHead>
                <TableHead className="w-[15%]">Platform Access</TableHead>
                <TableHead className="w-[15%]">Table Grants</TableHead>
                <TableHead className="w-[15%]">Users</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {roles.isLoading ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-24 text-center text-xs text-muted-foreground">
                    Loading roles...
                  </TableCell>
                </TableRow>
              ) : (
                (roles.data ?? []).map((r) => (
                  <TableRow key={r.id} className="hover:bg-muted/30">
                    <TableCell className="font-medium">
                      <span className="font-mono text-sm">{r.name}</span>
                      {r.isAdmin && (
                        <Badge variant="outline" className="ml-2 text-[10px] bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 border-indigo-500/20">
                          Builtin
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {r.platformManage ? (
                        <Badge variant="outline" className="text-[10px] bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20">
                          Full platform
                        </Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">Table CRUD only</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {r.isAdmin ? "all tables (implicit)" : `${r.tables.length} table${r.tables.length === 1 ? "" : "s"}`}
                    </TableCell>
                    <TableCell>
                      <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                        <UsersIcon className="h-3 w-3" /> {r.userCount}
                      </span>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {r.isAdmin ? (
                          <span className="text-[11px] text-muted-foreground italic">Protected</span>
                        ) : (
                          <>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-7 w-7 text-muted-foreground hover:text-foreground"
                              onClick={() => openEdit(r)}
                              title="Edit role"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-7 w-7 text-muted-foreground hover:text-destructive"
                              onClick={() => {
                                if (confirm(`Delete role "${r.name}"?`)) del.mutate(r.id);
                              }}
                              title="Delete role"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={!!form} onOpenChange={(o) => !o && setForm(null)}>
        <DialogContent className="max-w-3xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{form?.mode === "new" ? "Add Role" : `Edit Role: ${form?.name}`}</DialogTitle>
            <DialogDescription className="text-xs">
              Platform access grants Datasources, Tables &amp; Schema and Audit Trail management; table CRUD needs per-table grants
            </DialogDescription>
          </DialogHeader>
          {form && (
            <div className="space-y-5 py-1">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label className="text-xs font-medium">Role Name</Label>
                  <Input
                    className="h-9 text-xs"
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                    placeholder="e.g. Order Editor"
                  />
                </div>
                <div className="flex items-center justify-between rounded-lg border bg-muted/30 p-3 h-fit">
                  <div>
                    <p className="text-xs font-medium">Platform Management</p>
                    <p className="text-[11px] text-muted-foreground">Datasources, definitions, audit trail</p>
                  </div>
                  <Switch checked={form.platformManage} onCheckedChange={(v) => setForm({ ...form, platformManage: v })} />
                </div>
              </div>

              <div className="space-y-2">
                <Label className="text-xs font-medium">Table CRUD Grants</Label>
                <div className="rounded-lg border overflow-hidden">
                  <Table>
                    <TableHeader>
                      <TableRow className="bg-muted/50 hover:bg-muted/50">
                        <TableHead>Table</TableHead>
                        <TableHead className="text-center w-20">Read</TableHead>
                        <TableHead className="text-center w-20">Create</TableHead>
                        <TableHead className="text-center w-20">Update</TableHead>
                        <TableHead className="text-center w-20">Delete</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {(defs.data ?? []).length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={5} className="h-16 text-center text-xs text-muted-foreground">
                            No table definitions yet — grants can be added later
                          </TableCell>
                        </TableRow>
                      ) : (
                        (defs.data ?? []).map((d) => {
                          const g = grantFor(form, d.id);
                          return (
                            <TableRow key={d.id} className="hover:bg-muted/20">
                              <TableCell className="text-xs font-medium">{d.label}</TableCell>
                              <TableCell className="text-center">
                                <Switch checked={g.canRead} onCheckedChange={(v) => setForm(setGrant(form, d.id, { canRead: v }))} />
                              </TableCell>
                              <TableCell className="text-center">
                                <Switch checked={g.canCreate} onCheckedChange={(v) => setForm(setGrant(form, d.id, { canCreate: v }))} />
                              </TableCell>
                              <TableCell className="text-center">
                                <Switch checked={g.canUpdate} onCheckedChange={(v) => setForm(setGrant(form, d.id, { canUpdate: v }))} />
                              </TableCell>
                              <TableCell className="text-center">
                                <Switch checked={g.canDelete} onCheckedChange={(v) => setForm(setGrant(form, d.id, { canDelete: v }))} />
                              </TableCell>
                            </TableRow>
                          );
                        })
                      )}
                    </TableBody>
                  </Table>
                </div>
              </div>

              {save.isError && (
                <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive">
                  {(save.error as Error).message}: {String((save.error as { detail?: unknown }).detail ?? "")}
                </div>
              )}
            </div>
          )}
          <DialogFooter className="gap-2 sm:gap-0">
            <Button variant="outline" onClick={() => setForm(null)}>
              Cancel
            </Button>
            <Button
              onClick={() => save.mutate()}
              disabled={save.isPending || !form?.name}
              className="bg-blue-600 text-white hover:bg-blue-700"
            >
              {save.isPending ? "Saving..." : "Save Role"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
