import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Table2,
  Plus,
  Trash2,
  Edit,
  ExternalLink,
  Server,
  Layers,
  Sparkles,
} from "lucide-react";
import { api } from "../lib/api";
import type { Datasource, Me, TableDef } from "../lib/types";
import { useT } from "../lib/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function Tables() {
  const qc = useQueryClient();
  const t = useT();
  const defs = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/auth/me") });
  const canPlatform = !!me.data?.manageTables;
  const dsList = useQuery({ queryKey: ["ds"], queryFn: () => api<Datasource[]>("/datasources"), enabled: canPlatform });
  const [search, setSearch] = useState("");

  const filteredDefs = (defs.data ?? []).filter(
    (d) =>
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
            <CardTitle className="text-xs font-medium text-muted-foreground">{t("tables.statTables")}</CardTitle>
            <Table2 className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{defs.data?.length ?? 0}</div>
            <p className="text-[11px] text-muted-foreground mt-1">{t("tables.statTablesSub")}</p>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/60 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">{t("tables.statDs")}</CardTitle>
            <Server className="h-4 w-4 text-indigo-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{dsList.data?.length ?? 0}</div>
            <p className="text-[11px] text-muted-foreground mt-1">{t("tables.statDsSub")}</p>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/60 backdrop-blur-sm">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">{t("tables.statStatus")}</CardTitle>
            <Sparkles className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <span className="relative flex h-2.5 w-2.5">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-emerald-500"></span>
              </span>
              <span className="text-sm font-semibold text-emerald-600 dark:text-emerald-400">{t("tables.ready")}</span>
            </div>
            <p className="text-[11px] text-muted-foreground mt-1">{t("tables.statStatusSub")}</p>
          </CardContent>
        </Card>
      </div>

      {/* Main Table List Card */}
      <Card className="border-border/60 shadow-sm">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle className="text-base font-semibold">{t("tables.title")}</CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("tables.subtitle")}
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Input
              placeholder={t("tables.filter")}
              className="h-9 w-48 text-xs md:w-64"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {canPlatform && (
              <Link to="/tables/new">
                <Button className="bg-blue-600 text-white hover:bg-blue-700 shadow-xs">
                  <Plus className="h-4 w-4 mr-1.5" /> {t("tables.add")}
                </Button>
              </Link>
            )}
          </div>
        </CardHeader>

        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-[30%]">{t("tables.colLabel")}</TableHead>
                <TableHead className="w-[30%]">{t("tables.colTable")}</TableHead>
                <TableHead className="w-[20%]">{t("tables.colKey")}</TableHead>
                <TableHead className="text-right">{t("data.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {defs.isLoading ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center text-xs text-muted-foreground">
                    {t("tables.loading")}
                  </TableCell>
                </TableRow>
              ) : filteredDefs.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-32 text-center">
                    <div className="flex flex-col items-center justify-center space-y-2">
                      <Table2 className="h-8 w-8 text-muted-foreground/40" />
                      <p className="text-sm font-medium text-muted-foreground">{t("tables.empty")}</p>
                      <p className="text-xs text-muted-foreground/70">
                        {t("tables.emptyHint")}
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
                        {d.keyColumns.join(" + ")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Link to={`/data/${d.id}`}>
                          <Button variant="secondary" size="sm" className="h-8 gap-1 text-xs">
                            <ExternalLink className="h-3.5 w-3.5" /> {t("tables.viewData")}
                          </Button>
                        </Link>
                        {canPlatform && (
                          <>
                            <Link to={`/tables/${d.id}/edit`}>
                              <Button variant="outline" size="sm" className="h-8 gap-1 text-xs">
                                <Edit className="h-3.5 w-3.5" /> {t("btn.edit")}
                              </Button>
                            </Link>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-8 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
                              onClick={async () => {
                                if (!confirm(t("tables.deleteConfirm", { name: d.label }))) return;
                                await api(`/tables/${d.id}`, { method: "DELETE" });
                                qc.invalidateQueries({ queryKey: ["defs"] });
                              }}
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
    </div>
  );
}
