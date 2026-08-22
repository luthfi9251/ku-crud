import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Database,
  Table2,
  Server,
  ShieldCheck,
  ArrowLeftRight,
  LogOut,
  ChevronRight,
  ChevronDown,
  ChevronUp,
  Sparkles,
  Layers,
  PanelLeftClose,
  PanelLeft,
  Pencil,
  Plus,
  MoreVertical,
  Trash2,
  Users as UsersIcon,
  KeyRound,
  BookOpen,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { Me, TableDef, TableGroup } from "@/lib/types";
import { useT, useI18nLang, type Lang } from "@/lib/i18n";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

interface SidebarProps {
  className?: string;
}

export function Sidebar({ className }: SidebarProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [collapsed, setCollapsed] = useState(false);
  const t = useT();
  const { lang, setLang } = useI18nLang();

  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api<Me>("/auth/me"),
  });

  const defs = useQuery({
    queryKey: ["defs"],
    queryFn: () => api<TableDef[]>("/tables"),
  });

  const groups = useQuery({
    queryKey: ["groups"],
    queryFn: () => api<TableGroup[]>("/groups"),
  });

  const groupMut = useMutation({
    mutationFn: ({
      method,
      path,
      body,
    }: {
      method: string;
      path: string;
      body?: unknown;
    }) =>
      api(path, {
        method,
        body: body === undefined ? undefined : JSON.stringify(body),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["groups"] });
      queryClient.invalidateQueries({ queryKey: ["defs"] });
    },
    onError: (e) =>
      alert(
        e instanceof ApiError
          ? `${e.message}: ${String(e.detail ?? "")}`
          : t("nav.groupActionFailed"),
      ),
  });

  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(
    new Set(),
  );
  const toggleGroup = (id: string) =>
    setCollapsedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const [openMenu, setOpenMenu] = useState<string | null>(null);

  const handleLogout = async () => {
    try {
      await api("/auth/logout", { method: "POST" });
    } catch {
      // ignore
    } finally {
      queryClient.clear();
      navigate("/login");
    }
  };

  const isAdmin = !!me.data?.isAdmin;
  const canPlatform = !!me.data?.platformManage;
  const all = (defs.data ?? []).filter((t) => t.permissions?.read);
  const knownGroupIds = new Set((groups.data ?? []).map((g) => g.id));
  const byGroup = (gid?: string) =>
    all.filter((t) => {
      const eff =
        t.groupId && knownGroupIds.has(t.groupId) ? t.groupId : undefined;
      return (eff ?? "") === (gid ?? "");
    });

  const renderTable = (tb: TableDef) => {
    const tablePath = `/data/${tb.id}`;
    const isActive = location.pathname === tablePath;
    return (
      <div key={tb.id} className="relative">
        <Link
          to={tablePath}
          className={cn(
            "flex items-center justify-between rounded-lg px-3 py-2 text-xs transition-all group",
            isActive
              ? "bg-sidebar-accent text-sidebar-primary-foreground font-semibold"
              : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground",
          )}
          title={
            collapsed ? tb.label : `${tb.schemaName}.${tb.tableName}`
          }
        >
          <div className="flex items-center gap-2.5 truncate">
            <Layers
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                isActive ? "text-blue-400" : "text-sidebar-foreground/50",
              )}
            />
            {!collapsed && <span className="truncate">{tb.label}</span>}
          </div>
          {!collapsed && !canPlatform && (
            <ChevronRight className="h-3 w-3 opacity-0 group-hover:opacity-100 transition-opacity text-sidebar-foreground/40 shrink-0" />
          )}
        </Link>
        {canPlatform && !collapsed && (
          <div className="absolute right-1 top-1/2 -translate-y-1/2 z-10">
            <button
              onClick={() => setOpenMenu(openMenu === tb.id ? null : tb.id)}
              className="flex h-3.5 w-3.5 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
              title={t("nav.moveToGroup")}
            >
              <MoreVertical className="h-3 w-3" />
            </button>
            {openMenu === tb.id && (
              <div className="absolute right-0 z-10 mt-1 w-40 rounded-md border bg-popover p-1 shadow text-xs text-popover-foreground">
                {(groups.data ?? []).map((g) => (
                  <button
                    key={g.id}
                    className="block w-full px-2 py-1 text-left hover:bg-accent rounded-sm"
                    onClick={() => {
                      setOpenMenu(null);
                      groupMut.mutate({
                        method: "PATCH",
                        path: `/tables/${tb.id}`,
                        body: { groupId: g.id },
                      });
                    }}
                  >
                    {g.name}
                  </button>
                ))}
                <button
                  className="block w-full px-2 py-1 text-left hover:bg-accent rounded-sm"
                  onClick={() => {
                    setOpenMenu(null);
                    groupMut.mutate({
                      method: "PATCH",
                      path: `/tables/${tb.id}`,
                      body: { groupId: null },
                    });
                  }}
                >
                  {t("nav.noGroup")}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    );
  };
  const navItems = [
    ...(canPlatform
      ? [
          { label: t("nav.datasources"), path: "/datasources", icon: Server },
          { label: t("nav.transfer"), path: "/meta", icon: ArrowLeftRight },
        ]
      : []),
    { label: t("nav.tables"), path: "/", icon: Table2 },
    { label: t("nav.docs"), path: "/docs", icon: BookOpen },
    ...(isAdmin
      ? [
          { label: t("nav.users"), path: "/users", icon: UsersIcon },
          { label: t("nav.roles"), path: "/roles", icon: KeyRound },
        ]
      : []),
    ...(canPlatform
      ? [{ label: t("nav.audit"), path: "/audit", icon: ShieldCheck }]
      : []),
  ];

  return (
    <aside
      className={cn(
        "relative flex flex-col border-r bg-sidebar text-sidebar-foreground transition-all duration-300 ease-in-out select-none",
        collapsed ? "w-16" : "w-64",
        className,
      )}
    >
      {/* Brand Header */}
      <div className="flex h-16 items-center justify-between px-4 border-b border-sidebar-border/60">
        <div className="flex items-center gap-3 overflow-hidden">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500 to-indigo-600 text-white shadow-md shadow-blue-500/20">
            <Database className="h-5 w-5" />
          </div>
          {!collapsed && (
            <div className="flex flex-col truncate">
              <div className="flex items-center gap-1.5">
                <span className="font-bold tracking-tight text-sidebar-foreground">
                  Ku-CRUD
                </span>
                <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-blue-400 border border-blue-500/20">
                  v1.5
                </span>
              </div>
              <span className="text-[11px] text-sidebar-foreground/60 truncate">
                {t("nav.brandSubtitle")}
              </span>
            </div>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0 text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent"
          onClick={() => setCollapsed(!collapsed)}
          title={collapsed ? t("nav.expandSidebar") : t("nav.collapseSidebar")}
        >
          {collapsed ? (
            <PanelLeft className="h-4 w-4" />
          ) : (
            <PanelLeftClose className="h-4 w-4" />
          )}
        </Button>
      </div>

      {/* Navigation Body */}
      <div className="flex-1 overflow-y-auto px-3 py-4 space-y-6">
        {/* Main Navigation */}
        <div className="space-y-1">
          {!collapsed && (
            <p className="px-3 text-[10px] font-semibold uppercase tracking-wider text-sidebar-foreground/40 mb-2">
              {t("nav.section")}
            </p>
          )}
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = location.pathname === item.path;
            return (
              <Link
                key={item.path}
                to={item.path}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all group relative",
                  isActive
                    ? "bg-sidebar-accent text-sidebar-primary-foreground shadow-xs font-semibold"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground",
                )}
                title={collapsed ? item.label : undefined}
              >
                <Icon
                  className={cn(
                    "h-4 w-4 shrink-0 transition-transform group-hover:scale-110",
                    isActive ? "text-blue-400" : "text-sidebar-foreground/60",
                  )}
                />
                {!collapsed && <span>{item.label}</span>}
                {isActive && (
                  <div className="absolute right-0 top-2 bottom-2 w-1 rounded-l-full bg-blue-500" />
                )}
              </Link>
            );
          })}
        </div>

        <Separator className="bg-sidebar-border/50" />

        {/* Quick Access Tables */}
        <div className="space-y-1">
          {!collapsed && (
            <div className="flex items-center justify-between px-3 mb-2">
              <p className="text-[10px] font-semibold uppercase tracking-wider text-sidebar-foreground/40">
                {t("nav.activeTables", { count: String(defs.data?.length ?? 0) })}
              </p>
              <Sparkles className="h-3 w-3 text-blue-400/80" />
            </div>
          )}
          {defs.isLoading && !collapsed && (
            <div className="px-3 py-2 text-xs text-sidebar-foreground/40 italic">
              {t("nav.loadingTables")}
            </div>
          )}
          {collapsed ? (
            all.map(renderTable)
          ) : (
            <>
              {(groups.data ?? []).map((g) => (
                <div key={g.id}>
                  <div className="flex items-center justify-between px-2 py-1 group/gh">
                    <button
                      type="button"
                      onClick={() => toggleGroup(g.id)}
                      className="flex flex-1 items-center gap-1.5 min-w-0 text-left cursor-pointer hover:text-sidebar-foreground transition-colors py-0.5 rounded-sm hover:bg-sidebar-accent/30"
                      title={
                        collapsedGroups.has(g.id)
                          ? t("nav.expandGroup")
                          : t("nav.collapseGroup")
                      }
                    >
                      <div className="flex h-4 w-4 shrink-0 items-center justify-center text-sidebar-foreground/40 group-hover/gh:text-sidebar-foreground">
                        {collapsedGroups.has(g.id) ? (
                          <ChevronRight className="h-3 w-3" />
                        ) : (
                          <ChevronDown className="h-3 w-3" />
                        )}
                      </div>
                      <span className="truncate text-[10px] font-semibold uppercase tracking-wider text-sidebar-foreground/50 group-hover/gh:text-sidebar-foreground">
                        {g.name}
                      </span>
                    </button>
                    {canPlatform && (
                      <div className="flex items-center gap-0.5 shrink-0 opacity-80 group-hover/gh:opacity-100">
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            groupMut.mutate({
                              method: "PATCH",
                              path: `/groups/${g.id}`,
                              body: { move: "up" },
                            });
                          }}
                          className="flex h-4 w-4 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
                          title={t("nav.moveUp")}
                        >
                          <ChevronUp className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            groupMut.mutate({
                              method: "PATCH",
                              path: `/groups/${g.id}`,
                              body: { move: "down" },
                            });
                          }}
                          className="flex h-4 w-4 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
                          title={t("nav.moveDown")}
                        >
                          <ChevronDown className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            const name = prompt(t("nav.groupNamePrompt"), g.name);
                            if (name)
                              groupMut.mutate({
                                method: "PATCH",
                                path: `/groups/${g.id}`,
                                body: { name },
                              });
                          }}
                          className="flex h-4 w-4 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
                          title={t("nav.renameGroup")}
                        >
                          <Pencil className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            if (
                              confirm(
                                t("nav.deleteGroupConfirm", { name: g.name }),
                              )
                            )
                              groupMut.mutate({
                                method: "DELETE",
                                path: `/groups/${g.id}`,
                              });
                          }}
                          className="flex h-4 w-4 items-center justify-center rounded text-sidebar-foreground/40 hover:text-red-400 hover:bg-red-500/10"
                          title={t("nav.deleteGroup")}
                        >
                          <Trash2 className="h-3 w-3" />
                        </button>
                      </div>
                    )}
                  </div>
                  {!collapsedGroups.has(g.id) && byGroup(g.id).map(renderTable)}
                </div>
              ))}
              {canPlatform && (
                <button
                  onClick={() => {
                    const name = prompt(t("nav.groupNamePrompt"));
                    if (name)
                      groupMut.mutate({
                        method: "POST",
                        path: "/groups",
                        body: { name },
                      });
                  }}
                  className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-xs text-sidebar-foreground/50 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground transition-all"
                  title={t("nav.createGroupHint")}
                >
                  <Plus className="h-3 w-3" />
                  {t("nav.newGroup")}
                </button>
              )}
              {byGroup(undefined).map(renderTable)}
            </>
          )}
          {!defs.isLoading && (defs.data ?? []).length === 0 && !collapsed && (
            <div className="px-3 py-2 text-xs text-sidebar-foreground/40 italic">
              {t("nav.noTables")}
            </div>
          )}
        </div>
      </div>

      {/* Footer / User Profile */}
      <div className="border-t border-sidebar-border/60 p-3">
        {!collapsed && (
          <div className="mb-1 flex items-center justify-between px-1">
            <span className="text-[10px] text-sidebar-foreground/50">{t("app.language")}</span>
            <Select value={lang} onValueChange={(v) => setLang(v as Lang)}>
              <SelectTrigger className="h-6 w-24 text-[10px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="en" className="text-xs">English</SelectItem>
                <SelectItem value="id" className="text-xs">Indonesia</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}
        <div
          className={cn(
            "flex items-center justify-between rounded-xl bg-sidebar-accent/40 p-2",
            collapsed && "justify-center",
          )}
        >
          <div className="flex items-center gap-3 overflow-hidden">
            <Avatar className="h-8 w-8 border border-sidebar-border">
              <AvatarFallback className="bg-gradient-to-tr from-blue-600 to-indigo-600 text-white text-xs font-semibold">
                {me.data?.username
                  ? me.data.username.slice(0, 2).toUpperCase()
                  : "US"}
              </AvatarFallback>
            </Avatar>
            {!collapsed && (
              <div className="flex flex-col truncate">
                <span className="text-xs font-medium text-sidebar-foreground truncate">
                  {me.data?.username || t("nav.adminUser")}
                </span>
                <span className="text-[10px] text-sidebar-foreground/50">
                  {t("nav.online")}
                </span>
              </div>
            )}
          </div>
          {!collapsed && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-sidebar-foreground/60 hover:text-red-400 hover:bg-red-500/10"
              onClick={handleLogout}
              title={t("nav.logout")}
            >
              <LogOut className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </aside>
  );
}
