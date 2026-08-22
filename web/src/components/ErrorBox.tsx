import { humanError } from "../lib/errors";
import { useT } from "../lib/i18n";

// ErrorBox renders an error as a friendly sentence; the technical detail
// (code, raw message, payload) sits behind a collapsed disclosure.
export function ErrorBox({ e }: { e: unknown }) {
  const t = useT();
  const { title, detail } = humanError(e, t);
  return (
    <div className="rounded-lg bg-destructive/10 border border-destructive/30 p-3 text-xs text-destructive space-y-1">
      <p className="font-semibold">{title}</p>
      {detail && (
        <details className="text-[11px] opacity-80">
          <summary className="cursor-pointer select-none">{t("errors.technicalDetail")}</summary>
          <pre className="mt-1 whitespace-pre-wrap break-all font-mono">{detail}</pre>
        </details>
      )}
    </div>
  );
}
