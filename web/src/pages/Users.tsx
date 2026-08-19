import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Users as UsersIcon, Plus, KeyRound, Ban, Trash2 } from "lucide-react";
import { api } from "../lib/api";
import type { Role, User } from "../lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface UserForm {
  mode: "new" | "edit";
  user: User | null;
  username: string;
  password: string;
  roleId: string;
  disabled: boolean;
}

const emptyForm: UserForm = { mode: "new", user: null, username: "", password: "", roleId: "", disabled: false };

export default function Users() {
  const qc = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api<User[]>("/users") });
  const roles = useQuery({ queryKey: ["roles"], queryFn: () => api<Role[]>("/roles") });
  const [form, setForm] = useState<UserForm | null>(null);

  useEffect(() => {
    if (form && roles.data && !form.roleId) {
      const first = roles.data.find((r) => !r.isAdmin);
      setForm({ ...form, roleId: form.user?.roleId ?? first?.id ?? roles.data[0]?.id ?? "" });
    }
  }, [form, roles.data]);

  const save = useMutation({
    mutationFn: () => {
      if (form!.mode === "new") {
        return api("/users", {
          method: "POST",
          body: JSON.stringify({ username: form!.username, password: form!.password, roleId: form!.roleId }),
        });
      }
      const body: Record<string, unknown> = { roleId: form!.roleId, disabled: form!.disabled };
      if (form!.password) body.password = form!.password;
      return api(`/users/${form!.user!.id}`, { method: "PUT", body: JSON.stringify(body) });
    },
    onSuccess: () => {
      setForm(null);
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => api(`/users/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  return (
    <div className="space-y-6">
      <Card className="border-border/60 shadow-sm">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <UsersIcon className="h-4 w-4 text-blue-500" /> Users
            </CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              Manage login accounts and their role assignment
            </p>
          </div>
          <Button
            onClick={() => setForm({ ...emptyForm })}
            className="bg-blue-600 text-white hover:bg-blue-700 shadow-xs"
          >
            <Plus className="h-4 w-4 mr-1.5" /> Add User
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-[25%]">Username</TableHead>
                <TableHead className="w-[25%]">Role</TableHead>
                <TableHead className="w-[15%]">Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.isLoading ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center text-xs text-muted-foreground">
                    Loading users...
                  </TableCell>
                </TableRow>
              ) : (users.data ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center text-xs text-muted-foreground">
                    No users found
                  </TableCell>
                </TableRow>
              ) : (
                (users.data ?? []).map((u) => (
                  <TableRow key={u.id} className="hover:bg-muted/30">
                    <TableCell className="font-medium">
                      <span className="font-mono text-sm">{u.username}</span>
                      {u.isFirst && (
                        <Badge variant="outline" className="ml-2 text-[10px] bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20">
                          First User
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant="secondary"
                        className={`text-[11px] font-normal ${u.roleName === "Admin" ? "bg-indigo-500/10 text-indigo-600 dark:text-indigo-400" : ""}`}
                      >
                        {u.roleName}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {u.disabled ? (
                        <Badge variant="outline" className="text-[10px] bg-destructive/10 text-destructive border-destructive/20">
                          Disabled
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px] bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20">
                          Active
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {u.isFirst ? (
                          <span className="text-[11px] text-muted-foreground italic mr-2">Protected</span>
                        ) : (
                          <>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-7 w-7 text-muted-foreground hover:text-foreground"
                              onClick={() =>
                                setForm({
                                  mode: "edit",
                                  user: u,
                                  username: u.username,
                                  password: "",
                                  roleId: u.roleId,
                                  disabled: u.disabled,
                                })
                              }
                              title="Edit user"
                            >
                              <KeyRound className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-7 w-7 text-muted-foreground hover:text-destructive"
                              onClick={() => {
                                if (confirm(`Delete user "${u.username}"?`)) del.mutate(u.id);
                              }}
                              title="Delete user"
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
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{form?.mode === "new" ? "Add User" : `Edit User: ${form?.user?.username}`}</DialogTitle>
            <DialogDescription className="text-xs">
              {form?.mode === "new"
                ? "Create a login account and assign a role"
                : "Change password, role or disabled state (username is immutable)"}
            </DialogDescription>
          </DialogHeader>
          {form && (
            <div className="space-y-4 py-1">
              {form.mode === "new" && (
                <div className="space-y-1.5">
                  <Label className="text-xs font-medium">Username</Label>
                  <Input
                    className="h-9 text-xs"
                    value={form.username}
                    onChange={(e) => setForm({ ...form, username: e.target.value })}
                    placeholder="e.g. budi"
                  />
                </div>
              )}
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">
                  Password {form.mode === "edit" && <span className="text-muted-foreground">(leave blank to keep)</span>}
                </Label>
                <Input
                  type="password"
                  className="h-9 text-xs"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  placeholder="min 4 characters"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs font-medium">Role</Label>
                <Select value={form.roleId} onValueChange={(v) => setForm({ ...form, roleId: v })}>
                  <SelectTrigger className="h-9 text-xs">
                    <SelectValue placeholder="Choose a role..." />
                  </SelectTrigger>
                  <SelectContent>
                    {(roles.data ?? []).map((r) => (
                      <SelectItem key={r.id} value={r.id} className="text-xs">
                        {r.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {form.mode === "edit" && (
                <div className="flex items-center justify-between rounded-lg border bg-muted/30 p-3">
                  <div className="flex items-center gap-2">
                    <Ban className="h-4 w-4 text-muted-foreground" />
                    <div>
                      <p className="text-xs font-medium">Disabled</p>
                      <p className="text-[11px] text-muted-foreground">Blocked from logging in</p>
                    </div>
                  </div>
                  <Switch checked={form.disabled} onCheckedChange={(v) => setForm({ ...form, disabled: v })} />
                </div>
              )}
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
              disabled={save.isPending || !form?.roleId || (form.mode === "new" && (!form.username || form.password.length < 4))}
              className="bg-blue-600 text-white hover:bg-blue-700"
            >
              {save.isPending ? "Saving..." : "Save User"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
