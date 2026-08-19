import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Database,
  Table2,
  Server,
  ShieldCheck,
  LogOut,
  ChevronRight,
  Sparkles,
  Layers,
  PanelLeftClose,
  PanelLeft,
} from "lucide-react";
import { api } from "@/lib/api";
import type { TableDef } from "@/lib/types";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

interface SidebarProps {
  className?: string;
}

export function Sidebar({ className }: SidebarProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [collapsed, setCollapsed] = useState(false);

  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api<{ username: string }>("/auth/me"),
  });

  const defs = useQuery({
    queryKey: ["defs"],
    queryFn: () => api<TableDef[]>("/tables"),
  });

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

  const navItems = [
    { label: "Datasources", path: "/datasources", icon: Server },
    { label: "Tables & Schema", path: "/", icon: Table2 },
    { label: "Audit Trail", path: "/audit", icon: ShieldCheck },
  ];

  return (
    <aside
      className={cn(
        "relative flex flex-col border-r bg-sidebar text-sidebar-foreground transition-all duration-300 ease-in-out select-none",
        collapsed ? "w-16" : "w-64",
        className
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
                <span className="font-bold tracking-tight text-sidebar-foreground">Ku-CRUD</span>
                <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-blue-400 border border-blue-500/20">
                  v1.0
                </span>
              </div>
              <span className="text-[11px] text-sidebar-foreground/60 truncate">Database Portal</span>
            </div>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0 text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent"
          onClick={() => setCollapsed(!collapsed)}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {collapsed ? <PanelLeft className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
        </Button>
      </div>

      {/* Navigation Body */}
      <div className="flex-1 overflow-y-auto px-3 py-4 space-y-6">
        {/* Main Navigation */}
        <div className="space-y-1">
          {!collapsed && (
            <p className="px-3 text-[10px] font-semibold uppercase tracking-wider text-sidebar-foreground/40 mb-2">
              Navigation
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
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                )}
                title={collapsed ? item.label : undefined}
              >
                <Icon className={cn("h-4 w-4 shrink-0 transition-transform group-hover:scale-110", isActive ? "text-blue-400" : "text-sidebar-foreground/60")} />
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
                Active Tables ({defs.data?.length ?? 0})
              </p>
              <Sparkles className="h-3 w-3 text-blue-400/80" />
            </div>
          )}
          {defs.isLoading && !collapsed && (
            <div className="px-3 py-2 text-xs text-sidebar-foreground/40 italic">Loading tables...</div>
          )}
          {(defs.data ?? []).map((table) => {
            const tablePath = `/data/${table.id}`;
            const isActive = location.pathname === tablePath;
            return (
              <Link
                key={table.id}
                to={tablePath}
                className={cn(
                  "flex items-center justify-between rounded-lg px-3 py-2 text-xs transition-all group",
                  isActive
                    ? "bg-sidebar-accent text-sidebar-primary-foreground font-semibold"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                )}
                title={collapsed ? table.label : `${table.schemaName}.${table.tableName}`}
              >
                <div className="flex items-center gap-2.5 truncate">
                  <Layers className={cn("h-3.5 w-3.5 shrink-0", isActive ? "text-blue-400" : "text-sidebar-foreground/50")} />
                  {!collapsed && <span className="truncate">{table.label}</span>}
                </div>
                {!collapsed && (
                  <ChevronRight className="h-3 w-3 opacity-0 group-hover:opacity-100 transition-opacity text-sidebar-foreground/40 shrink-0" />
                )}
              </Link>
            );
          })}
          {!defs.isLoading && (defs.data ?? []).length === 0 && !collapsed && (
            <div className="px-3 py-2 text-xs text-sidebar-foreground/40 italic">No tables defined yet</div>
          )}
        </div>
      </div>

      {/* Footer / User Profile */}
      <div className="border-t border-sidebar-border/60 p-3">
        <div className={cn("flex items-center justify-between rounded-xl bg-sidebar-accent/40 p-2", collapsed && "justify-center")}>
          <div className="flex items-center gap-3 overflow-hidden">
            <Avatar className="h-8 w-8 border border-sidebar-border">
              <AvatarFallback className="bg-gradient-to-tr from-blue-600 to-indigo-600 text-white text-xs font-semibold">
                {me.data?.username ? me.data.username.slice(0, 2).toUpperCase() : "US"}
              </AvatarFallback>
            </Avatar>
            {!collapsed && (
              <div className="flex flex-col truncate">
                <span className="text-xs font-medium text-sidebar-foreground truncate">
                  {me.data?.username || "Admin User"}
                </span>
                <span className="text-[10px] text-sidebar-foreground/50">Online</span>
              </div>
            )}
          </div>
          {!collapsed && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-sidebar-foreground/60 hover:text-red-400 hover:bg-red-500/10"
              onClick={handleLogout}
              title="Logout"
            >
              <LogOut className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </aside>
  );
}
