import { useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Upload, ArrowLeft, CheckCircle2, XCircle, FileSpreadsheet } from "lucide-react";
import { api, ApiError } from "../lib/api";
import type { TableDefPayload } from "../lib/types";
import { useT } from "../lib/i18n";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

interface ImportRowError {
  column: string;
  message: string;
}

interface ImportRow {
  values: Record<string, string>;
  valid: boolean;
  errors?: ImportRowError[];
}

interface PreviewRes {
  delimiter: string;
  headers: string[];
  mapping: Record<string, string>;
  counts: { total: number; valid: number; invalid: number };
  rows: ImportRow[];
}

interface ApplyRes {
  inserted: number;
  failed: number;
  failures: { row: number; errors: ImportRowError[] }[];
}

export default function Import() {
  const { id } = useParams();
  const qc = useQueryClient();
  const t = useT();
  const fileRef = useRef<HTMLInputElement>(null);

  const def = useQuery({
    queryKey: ["def", id],
    queryFn: () => api<TableDefPayload>(`/tables/${id}`),
  });

  const [file, setFile] = useState<File | null>(null);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<PreviewRes | null>(null);
  const [result, setResult] = useState<ApplyRes | null>(null);
  const [err, setErr] = useState("");

  const upload = useMutation({
    mutationFn: async (f: File) => {
      const fd = new FormData();
      fd.append("file", f);
      return api<PreviewRes>(`/tables/${id}/import/preview`, { method: "POST", body: fd });
    },
    onSuccess: (res) => {
      setPreview(res);
      setMapping(res.mapping);
      setResult(null);
      setErr("");
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const repreview = useMutation({
    mutationFn: async () => {
      if (!file) throw new Error("no file");
      const fd = new FormData();
      fd.append("file", file);
      fd.append("mapping", JSON.stringify(mapping));
      return api<PreviewRes>(`/tables/${id}/import/preview`, { method: "POST", body: fd });
    },
    onSuccess: (res) => {
      setPreview(res);
      setErr("");
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const apply = useMutation({
    mutationFn: async (mode: "valid" | "all") => {
      if (!file) throw new Error("no file");
      const fd = new FormData();
      fd.append("file", file);
      fd.append("mapping", JSON.stringify(mapping));
      fd.append("mode", mode);
      return api<ApplyRes>(`/tables/${id}/import/apply`, { method: "POST", body: fd });
    },
    onSuccess: (res) => {
      setResult(res);
      qc.invalidateQueries({ queryKey: ["rows", id] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const mappableCols = (def.data?.columns ?? []).filter((c) => c.editable || (def.data?.keyColumns ?? []).includes(c.name));
  const validCount = preview?.counts.valid ?? 0;
  const totalCount = preview?.counts.total ?? 0;

  return (
    <div className="space-y-6 pb-12">
      <div className="flex items-center justify-between border-b pb-4">
        <div>
          <h2 className="text-lg font-semibold">{t("imp.title", { label: def.data?.label ?? "..." })}</h2>
          <p className="text-xs text-muted-foreground">
            {t("imp.subtitle")}
          </p>
        </div>
        <Link to={`/data/${id}`}>
          <Button variant="outline" size="sm" className="gap-1 text-xs">
            <ArrowLeft className="h-3.5 w-3.5" /> {t("imp.back")}
          </Button>
        </Link>
      </div>

      {err && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{err}</div>
      )}

      {/* Step 1: upload */}
      <Card>
        <CardHeader>
           <CardTitle className="text-sm font-semibold">{t("imp.step1")}</CardTitle>
           <CardDescription className="text-xs">{t("imp.step1Desc")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center gap-3">
          <input
            ref={fileRef}
            type="file"
            accept=".csv,text/csv"
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
            <Upload className="h-3.5 w-3.5" /> {upload.isPending ? t("imp.parsing") : t("imp.chooseCsv")}
          </Button>
          {file && (
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <FileSpreadsheet className="h-3.5 w-3.5" /> {file.name} ({(file.size / 1024).toFixed(1)} KB)
              {preview && <Badge variant="secondary" className="ml-2">{t("imp.delimiter", { value: preview.delimiter })}</Badge>}
            </span>
          )}
        </CardContent>
      </Card>

      {/* Step 2: mapping + preview */}
      {preview && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("imp.step2")}</CardTitle>
            <CardDescription className="text-xs">
              {t("imp.step2Desc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
              {preview.headers.map((h) => {
                const colName = mapping[h] ?? "";
                const colLabel = mappableCols.find((c) => c.name === colName)?.label ?? colName;
                return (
                  <div key={h} className="space-y-1">
                    <Label className="text-[11px] font-mono text-muted-foreground">{h}</Label>
                    <Select
                      value={colName || "ignore"}
                      onValueChange={(v) => {
                        setMapping((m) => ({ ...m, [h]: v === "ignore" ? "" : v }));
                      }}
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder={t("imp.ignore")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="ignore" className="text-xs">{t("imp.ignore")}</SelectItem>
                        {mappableCols.map((c) => (
                          <SelectItem key={c.name} value={c.name} className="text-xs">
                            {c.label} ({c.name})
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {colName && colLabel !== h && <p className="text-[10px] text-muted-foreground">→ {colLabel}</p>}
                  </div>
                );
              })}
            </div>

            <div className="flex flex-wrap items-center gap-2 pt-1">
              <Button variant="outline" size="sm" className="text-xs" onClick={() => repreview.mutate()} disabled={repreview.isPending}>
                {repreview.isPending ? t("imp.revalidating") : t("imp.revalidate")}
              </Button>
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {t("imp.validCount", { count: String(validCount) })}
              </Badge>
              {preview.counts.invalid > 0 && (
                <Badge variant="outline" className="gap-1 bg-red-500/10 text-red-600 border-red-500/20">
                  <XCircle className="h-3 w-3" /> {t("imp.invalidCount", { count: String(preview.counts.invalid) })}
                </Badge>
              )}
              <Badge variant="outline">{t("imp.rowsCount", { count: String(totalCount) })}</Badge>
            </div>

            <div className="max-h-[420px] overflow-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10 text-[11px]">#</TableHead>
                     <TableHead className="w-16 text-[11px]">{t("imp.status")}</TableHead>
                    {preview.headers.map((h) => (
                      <TableHead key={h} className="text-[11px] font-mono">{h}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {preview.rows.slice(0, 100).map((row, i) => (
                    <TableRow key={i} className={row.valid ? "" : "bg-red-500/5"}>
                      <TableCell className="text-[11px] text-muted-foreground">{i + 1}</TableCell>
                      <TableCell>
                        {row.valid ? (
                          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
                        ) : (
                          <span title={(row.errors ?? []).map((e) => `${e.column}: ${e.message}`).join("\n")}>
                            <XCircle className="h-3.5 w-3.5 text-red-500" />
                          </span>
                        )}
                      </TableCell>
                      {preview.headers.map((h) => (
                        <TableCell key={h} className="max-w-[200px] truncate text-[11px] font-mono">
                          {row.values[h]}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
             {totalCount > 100 && (
              <p className="text-[11px] text-muted-foreground">{t("imp.previewNote", { count: String(totalCount) })}</p>
            )}
            {preview.rows.some((r) => !r.valid) && (
              <div className="max-h-40 overflow-auto rounded-md border border-red-500/20 bg-red-500/5 p-3">
                <ul className="space-y-1 text-[11px] text-red-600 dark:text-red-400">
                  {preview.rows.map((r, i) =>
                    r.valid ? null : (
                       <li key={i}>
                         <span className="font-mono font-semibold">{t("imp.row", { number: String(i + 1) })}</span>{" "}
                        {(r.errors ?? []).map((e) => `${e.column || "?"} — ${e.message}`).join("; ")}
                      </li>
                    )
                  )}
                </ul>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Step 3: apply */}
      {preview && !result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">{t("imp.step3")}</CardTitle>
            <CardDescription className="text-xs">
              {t("imp.step3Desc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button
              size="sm"
              className="bg-blue-600 text-white hover:bg-blue-700 gap-1 text-xs"
              disabled={validCount === 0 || apply.isPending}
              onClick={() => apply.mutate("valid")}
            >
              <CheckCircle2 className="h-3.5 w-3.5" /> {t("imp.insertValid", { count: String(validCount) })}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="gap-1 text-xs"
              disabled={totalCount === 0 || apply.isPending}
              onClick={() => apply.mutate("all")}
            >
              {t("imp.insertAll", { count: String(totalCount) })}
            </Button>
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
            <div className="flex gap-2">
              <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                <CheckCircle2 className="h-3 w-3" /> {t("imp.inserted", { count: String(result.inserted) })}
              </Badge>
              {result.failed > 0 && (
                <Badge variant="outline" className="gap-1 bg-red-500/10 text-red-600 border-red-500/20">
                  <XCircle className="h-3 w-3" /> {t("imp.failed", { count: String(result.failed) })}
                </Badge>
              )}
            </div>
            {result.failures.length > 0 && (
              <ul className="space-y-1 rounded-md border border-red-500/20 bg-red-500/5 p-3 text-[11px] text-red-600 dark:text-red-400">
                {result.failures.slice(0, 200).map((f, i) => (
                 <li key={i}>
                   <span className="font-mono font-semibold">{t("imp.row", { number: String(f.row + 1) })}</span>{" "}
                    {f.errors.map((e) => `${e.column ? e.column + " — " : ""}${e.message}`).join("; ")}
                  </li>
                ))}
              </ul>
            )}
             <Link to={`/data/${id}`}>
               <Button size="sm" className="bg-blue-600 text-white hover:bg-blue-700 text-xs">
                 {t("imp.backGrid")}
               </Button>
             </Link>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
