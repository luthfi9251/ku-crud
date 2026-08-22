import { ApiError } from "./api";

type T = (key: string, vars?: Record<string, string>) => string;

// humanError maps an ApiError code to a friendly i18n sentence. Unknown
// codes fall back to the raw server message. `detail` always carries the
// technical payload (code/message/detail JSON) for the disclosure UI.
export function humanError(e: unknown, t: T): { title: string; detail: string } {
  if (e instanceof ApiError) {
    const key = `errors.${e.code}`;
    const title = t(key, { msg: e.message });
    return {
      title: title === key ? e.message : title,
      detail: JSON.stringify({ code: e.code, message: e.message, detail: e.detail }),
    };
  }
  return { title: e instanceof Error ? e.message : String(e), detail: "" };
}
