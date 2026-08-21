import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Upload, CheckCircle2, XCircle, Download, FileJson } from "lucide-react";
import { api, ApiError } from "../lib/api";
import type { Me } from "../lib/types";
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
        alert(`Export failed: ${msg}`);
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
    return <div className="p-8 text-sm text-muted-foreground">Platform Management access required.</div>;
  }

  const anySelected =
    (preview?.datasources ?? []).some((d) => (dsMode[d.ref] ?? "overwrite") !== "skip") ||
    (preview?.tables ?? []).some((t) => (tblMode[t.ref] ?? "overwrite") !== "skip");

  return (
    <div className="space-y-6 pb-12">
      <div className="border-b pb-4">
        <h2 className="text-lg font-semibold">Definitions Transfer</h2>
        <p className="text-xs text-muted-foreground">
          Export every table definition, datasource (connection settings only — passwords are never
          exported) and sidebar group as JSON, or import such a file from another instance.
        </p>
      </div>

      {err && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{err}</div>
      )}

      {/* Export */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold">Export</CardTitle>
          <CardDescription className="text-xs">
            Downloads a ku-crud-meta JSON snapshot of this instance's metadata. No passwords are included.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" size="sm" className="gap-1 text-xs" onClick={exportMeta} disabled={exporting}>
            <Download className="h-3.5 w-3.5" /> {exporting ? "Exporting..." : "Download definitions JSON"}
          </Button>
        </CardContent>
      </Card>

      {/* Step 1: upload */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold">1. Upload File</CardTitle>
          <CardDescription className="text-xs">Choose a ku-crud-meta JSON file to import (max 2 MB)</CardDescription>
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
            <Upload className="h-3.5 w-3.5" /> {upload.isPending ? "Analyzing..." : "Choose JSON file"}
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
            <CardTitle className="text-sm font-semibold">2. Review &amp; Select</CardTitle>
            <CardDescription className="text-xs">
              New and conflicting items default to overwrite; identical duplicates default to skip.
              New datasources need a password before they can be created.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                Datasources ({preview.datasources.length})
              </h3>
              <div className="overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-[11px]">Ref</TableHead>
                      <TableHead className="text-[11px]">Status</TableHead>
                      <TableHead className="text-[11px]">Mode</TableHead>
                      <TableHead className="text-[11px]">Password</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.datasources.map((d) => (
                      <TableRow key={d.ref}>
                        <TableCell className="text-[11px] font-mono">
                          {d.ref}
                          {(d.conflicts ?? []).length > 0 && (
                            <p className="whitespace-normal text-[10px] text-amber-600 dark:text-amber-400">
                              conflicts: {(d.conflicts ?? []).join(", ")}
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
                              <SelectItem value="skip" className="text-xs">skip</SelectItem>
                              <SelectItem value="overwrite" className="text-xs">overwrite</SelectItem>
                            </SelectContent>
                          </Select>
                        </TableCell>
                        <TableCell>
                          <Input
                            type="password"
                            className="h-8 w-44 text-xs"
                            placeholder={d.status === "new" ? "password" : "— existing datasource —"}
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
                          No datasources in file
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>

            <div className="space-y-2">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                Tables ({preview.tables.length})
              </h3>
              <div className="overflow-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-[11px]">Ref</TableHead>
                      <TableHead className="text-[11px]">Status</TableHead>
                      <TableHead className="text-[11px]">Mode</TableHead>
                      <TableHead className="text-[11px]">Dependencies</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.tables.map((t) => (
                      <TableRow key={t.ref} className={t.invalid ? "bg-red-500/5" : ""}>
                        <TableCell className="max-w-[280px] text-[11px] font-mono">
                          {t.ref}
                          {t.invalid && t.reason && (
                            <p className="whitespace-normal text-[10px] text-red-600 dark:text-red-400">
                              <XCircle className="mr-0.5 inline h-3 w-3" />
                              {t.reason}
                            </p>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className={cn("text-[10px]", statusVariant(t.status))}>
                            {statusLabel(t.status)}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {t.invalid ? (
                            <span className="text-[11px] text-muted-foreground" title="Invalid tables cannot be imported">
                              skip (forced)
                            </span>
                          ) : (
                            <Select
                              value={tblMode[t.ref] ?? "overwrite"}
                              onValueChange={(v) => setTblMode((m) => ({ ...m, [t.ref]: v }))}
                            >
                              <SelectTrigger className="h-8 w-28 text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="skip" className="text-xs">skip</SelectItem>
                                <SelectItem value="overwrite" className="text-xs">overwrite</SelectItem>
                              </SelectContent>
                            </Select>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex max-w-[320px] flex-wrap gap-1">
                            {t.dependencies.length === 0 && (
                              <span className="text-[10px] text-muted-foreground">—</span>
                            )}
                            {t.dependencies.map((dep) => (
                              <span
                                key={dep.ref}
                                title={dep.resolved ? "resolved (local or in file)" : "unresolved"}
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
                          No tables in file
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
            <CardTitle className="text-sm font-semibold">3. Apply</CardTitle>
            <CardDescription className="text-xs">
              Runs in a single transaction — either everything applies or nothing changes. Every write is audited.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-4">
            <div className="flex items-center gap-2">
              <Checkbox id="import-groups" checked={groupsSel} onChange={(e) => setGroupsSel(e.target.checked)} />
              <Label htmlFor="import-groups" className="text-xs text-muted-foreground">
                Import table groups
              </Label>
            </div>
            <Button
              size="sm"
              className="bg-blue-600 text-white hover:bg-blue-700 gap-1 text-xs"
              disabled={!anySelected || apply.isPending}
              onClick={() => apply.mutate()}
            >
              <CheckCircle2 className="h-3.5 w-3.5" /> {apply.isPending ? "Importing..." : "Import selected definitions"}
            </Button>
            {!anySelected && (
              <span className="text-[11px] text-muted-foreground">Everything is skipped — nothing to import.</span>
            )}
          </CardContent>
        </Card>
      )}

      {/* Result */}
      {result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">Result</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {result.createdDefs} definitions created
              </Badge>
              <Badge variant="secondary" className="gap-1 bg-blue-500/10 text-blue-600 border-blue-500/20">
                {result.updatedDefs} definitions updated
              </Badge>
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {result.groupsCreated} groups created
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              Datasource passwords are not imported — verify connections before use (drift check on
              each table is available from Tables &amp; Schema).
            </p>
            <Link to="/">
              <Button size="sm" className="bg-blue-600 text-white hover:bg-blue-700 text-xs">
                Back to Tables
              </Button>
            </Link>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
