import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Upload, CheckCircle2, XCircle, Download, FileJson } from "lucide-react";
import { api, ApiError } from "../lib/api";
import type { Me } from "../lib/types";
import { useT } from "../lib/i18n";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

interface DsItem {
  ref: string;
  status: string;
  conflicts?: string[];
}
interface TblItem {
  ref: string;
  status: string;
  invalid?: boolean;
  reason?: string;
  dependencies: { ref: string; resolved: boolean }[];
}
interface PreviewRes {
  datasources: DsItem[];
  tables: TblItem[];
}
interface ApplyRes {
  createdDefs: number;
  updatedDefs: number;
  groupsCreated: number;
}

const statusVariant = (s: string) =>
  s === "new"
    ? "bg-blue-500/10 text-blue-600 border-blue-500/20"
    : s === "duplicate-identical"
      ? "bg-emerald-500/10 text-emerald-600 border-emerald-500/20"
      : s === "duplicate-conflicts"
        ? "bg-amber-500/10 text-amber-600 border-amber-500/20"
        : "bg-destructive/10 text-destructive border-destructive/20";

const statusLabel = (s: string) => s.replace(/-/g, " ");

export default function MetaTransfer() {
  const qc = useQueryClient();
  const t = useT();
  const fileRef = useRef<HTMLInputElement>(null);

  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api<Me>("/auth/me"),
  });

  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<PreviewRes | null>(null);
  const [dsMode, setDsMode] = useState<Record<string, string>>({});
  const [dsPassword, setDsPassword] = useState<Record<string, string>>({});
  const [tblMode, setTblMode] = useState<Record<string, string>>({});
  const [groupsSel, setGroupsSel] = useState(true);
  const [result, setResult] = useState<ApplyRes | null>(null);
  const [err, setErr] = useState("");
  const [exporting, setExporting] = useState(false);

  const upload = useMutation({
    mutationFn: async (f: File) => {
      const fd = new FormData();
      fd.append("file", f);
      return api<PreviewRes>("/meta/import/preview", { method: "POST", body: fd });
    },
    onSuccess: (res) => {
      setPreview(res);
      setResult(null);
      setErr("");
      const dm: Record<string, string> = {};
      for (const d of res.datasources) dm[d.ref] = d.status === "duplicate-identical" ? "skip" : "overwrite";
      setDsMode(dm);
      setDsPassword({});
      const tm: Record<string, string> = {};
      for (const t of res.tables) tm[t.ref] = t.invalid || t.status === "duplicate-identical" ? "skip" : "overwrite";
      setTblMode(tm);
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const apply = useMutation({
    mutationFn: async () => {
      if (!file || !preview) throw new Error("no file");
      const selections = {
        datasources: preview.datasources
          .filter((d) => (dsMode[d.ref] ?? "overwrite") !== "skip")
          .map((d) => ({ ref: d.ref, mode: dsMode[d.ref] ?? "overwrite", password: dsPassword[d.ref] ?? "" })),
        tables: preview.tables
          .filter((t) => (tblMode[t.ref] ?? "overwrite") !== "skip")
          .map((t) => ({ ref: t.ref, mode: tblMode[t.ref] ?? "overwrite" })),
        groups: groupsSel,
      };
      const fd = new FormData();
      fd.append("file", file);
      fd.append("selections", JSON.stringify(selections));
      return api<ApplyRes>("/meta/import/apply", { method: "POST", body: fd });
    },
    onSuccess: (res) => {
      setResult(res);
      setErr("");
      qc.invalidateQueries({ queryKey: ["defs"] });
      qc.invalidateQueries({ queryKey: ["groups"] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const exportMeta = async () => {
    setExporting(true);
    try {
      const res = await fetch(`/api/meta/export`, { credentials: "same-origin" });
      if (!res.ok) {
        let msg = `HTTP ${res.status}`;
        try {
          const e = await res.json();
          msg = e.message ? `${e.message}` : msg;
        } catch {
          /* not json */
        }
        alert(t("data.exportFailed", { msg }));
        return;
      }
      const cd = res.headers.get("Content-Disposition") || "";
      const m = /filename="?([^";]+)"?/.exec(cd);
      const name = m?.[1] ?? `ku-crud-meta.json`;
      const url = URL.createObjectURL(await res.blob());
      const a = document.createElement("a");
      a.href = url;
      a.download = name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } finally {
      setExporting(false);
    }
  };

  if (me.data && !me.data.platformManage) {
    return <div className="p-8 text-sm text-muted-foreground">{t("meta.accessRequired")}</div>;
  }

  const anySelected =
    (preview?.datasources ?? []).some((d) => (dsMode[d.ref] ?? "overwrite") !== "skip") ||
    (preview?.tables ?? []).some((t) => (tblMode[t.ref] ?? "overwrite") !== "skip");

  return (
    <div className="space-y-6 pb-12">
      <div className="border-b pb-4">
        <h2 className="text-lg font-semibold">{t("meta.title")}</h2>
        <p className="text-xs text-muted-foreground">
          {t("meta.subtitle")}
        </p>
      </div>

      {err && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{err}</div>
      )}

      {/* Export */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold">{t("meta.export")}</CardTitle>
          <CardDescription className="text-xs">
            {t("meta.exportDesc")}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" size="sm" className="gap-1 text-xs" onClick={exportMeta} disabled={exporting}>
            <Download className="h-3.5 w-3.5" /> {exporting ? t("meta.exporting") : t("meta.download")}
          </Button>
        </CardContent>
      </Card>

      {/* Step 1: upload */}
      <Card>
        <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("meta.step1")}</CardTitle>
            <CardDescription className="text-xs">{t("meta.step1Desc")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center gap-3">
          <input
            ref={fileRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0] ?? null;
              setFile(f);
              setPreview(null);
              setResult(null);
              if (f) upload.mutate(f);
            }}
          />
          <Button variant="outline" size="sm" className="gap-1 text-xs" onClick={() => fileRef.current?.click()} disabled={upload.isPending}>
            <Upload className="h-3.5 w-3.5" /> {upload.isPending ? t("meta.analyzing") : t("meta.chooseJson")}
          </Button>
          {file && (
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <FileJson className="h-3.5 w-3.5" /> {file.name} ({(file.size / 1024).toFixed(1)} KB)
            </span>
          )}
        </CardContent>
      </Card>

      {/* Step 2: review + select */}
      {preview && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("meta.step2")}</CardTitle>
            <CardDescription className="text-xs">
              {t("meta.step2Desc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                {t("meta.datasources", { count: String(preview.datasources.length) })}
              </h3>
              <div className="overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-[11px]">{t("meta.ref")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.status")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.mode")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.password")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.datasources.map((d) => (
                      <TableRow key={d.ref}>
                        <TableCell className="text-[11px] font-mono">
                          {d.ref}
                          {(d.conflicts ?? []).length > 0 && (
                            <p className="whitespace-normal text-[10px] text-amber-600 dark:text-amber-400">
                              {t("meta.conflicts", { list: (d.conflicts ?? []).join(", ") })}
                            </p>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className={cn("text-[10px]", statusVariant(d.status))}>
                            {statusLabel(d.status)}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Select
                            value={dsMode[d.ref] ?? "overwrite"}
                            onValueChange={(v) => setDsMode((m) => ({ ...m, [d.ref]: v }))}
                          >
                            <SelectTrigger className="h-8 w-28 text-xs">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="skip" className="text-xs">{t("meta.skip")}</SelectItem>
                              <SelectItem value="overwrite" className="text-xs">{t("meta.overwrite")}</SelectItem>
                            </SelectContent>
                          </Select>
                        </TableCell>
                        <TableCell>
                          <Input
                            type="password"
                            className="h-8 w-44 text-xs"
                            placeholder={d.status === "new" ? t("meta.passwordPh") : t("meta.existingDs")}
                            disabled={d.status !== "new"}
                            value={dsPassword[d.ref] ?? ""}
                            onChange={(e) => setDsPassword((p) => ({ ...p, [d.ref]: e.target.value }))}
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                    {preview.datasources.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={4} className="py-3 text-center text-[11px] text-muted-foreground">
                          {t("meta.noDs")}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>

            <div className="space-y-2">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                {t("meta.tables", { count: String(preview.tables.length) })}
              </h3>
              <div className="overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-[11px]">{t("meta.ref")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.status")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.mode")}</TableHead>
                      <TableHead className="text-[11px]">{t("meta.dependencies")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.tables.map((tbl) => (
                      <TableRow key={tbl.ref} className={tbl.invalid ? "bg-red-500/5" : ""}>
                        <TableCell className="max-w-[280px] text-[11px] font-mono">
                          {tbl.ref}
                          {tbl.invalid && tbl.reason && (
                            <p className="whitespace-normal text-[10px] text-red-600 dark:text-red-400">
                              <XCircle className="mr-0.5 inline h-3 w-3" />
                              {tbl.reason}
                            </p>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className={cn("text-[10px]", statusVariant(tbl.status))}>
                            {statusLabel(tbl.status)}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {tbl.invalid ? (
                            <span className="text-[11px] text-muted-foreground" title={t("meta.invalidTitle")}>
                              {t("meta.skipForced")}
                            </span>
                          ) : (
                            <Select
                              value={tblMode[tbl.ref] ?? "overwrite"}
                              onValueChange={(v) => setTblMode((m) => ({ ...m, [tbl.ref]: v }))}
                            >
                              <SelectTrigger className="h-8 w-28 text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="skip" className="text-xs">{t("meta.skip")}</SelectItem>
                                <SelectItem value="overwrite" className="text-xs">{t("meta.overwrite")}</SelectItem>
                              </SelectContent>
                            </Select>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex max-w-[320px] flex-wrap gap-1">
                            {tbl.dependencies.length === 0 && (
                              <span className="text-[10px] text-muted-foreground">—</span>
                            )}
                            {tbl.dependencies.map((dep) => (
                              <span
                                key={dep.ref}
                                title={dep.resolved ? t("meta.resolved") : t("meta.unresolved")}
                                className={cn(
                                  "rounded border px-1.5 py-0.5 font-mono text-[10px]",
                                  dep.resolved
                                    ? "border-emerald-500/20 bg-emerald-500/10 text-emerald-600"
                                    : "border-red-500/20 bg-red-500/10 text-red-600",
                                )}
                              >
                                {dep.ref}
                              </span>
                            ))}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                    {preview.tables.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={4} className="py-3 text-center text-[11px] text-muted-foreground">
                          {t("meta.noTables")}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Step 3: apply */}
      {preview && !result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("meta.step3")}</CardTitle>
            <CardDescription className="text-xs">
              {t("meta.step3Desc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-4">
            <div className="flex items-center gap-2">
              <Checkbox id="import-groups" checked={groupsSel} onChange={(e) => setGroupsSel(e.target.checked)} />
              <Label htmlFor="import-groups" className="text-xs text-muted-foreground">
                {t("meta.importGroups")}
              </Label>
            </div>
            <Button
              size="sm"
              className="bg-blue-600 text-white hover:bg-blue-700 gap-1 text-xs"
              disabled={!anySelected || apply.isPending}
              onClick={() => apply.mutate()}
            >
              <CheckCircle2 className="h-3.5 w-3.5" /> {apply.isPending ? t("meta.importing") : t("meta.importSel")}
            </Button>
            {!anySelected && (
              <span className="text-[11px] text-muted-foreground">{t("meta.nothingToImport")}</span>
            )}
          </CardContent>
        </Card>
      )}

      {/* Result */}
      {result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("imp.result")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {t("meta.defsCreated", { count: String(result.createdDefs) })}
              </Badge>
              <Badge variant="secondary" className="gap-1 bg-blue-500/10 text-blue-600 border-blue-500/20">
                {t("meta.defsUpdated", { count: String(result.updatedDefs) })}
              </Badge>
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {t("meta.groupsCreated", { count: String(result.groupsCreated) })}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("meta.passwordsNote")}
            </p>
            <Link to="/">
              <Button size="sm" className="bg-blue-600 text-white hover:bg-blue-700 text-xs">
                {t("data.backToTables")}
              </Button>
            </Link>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
