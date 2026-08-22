import { createContext, useContext, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { Me } from "../types";
import en from "./en";
import id from "./id";

export type Lang = "en" | "id";
const EN: Record<string, string> = en;
const DICTS: Record<Lang, Record<string, string>> = { en, id };

interface LangCtx {
  lang: Lang;
  setLang: (l: Lang) => void;
}
const Ctx = createContext<LangCtx>({ lang: "en", setLang: () => {} });

// override remembers a language explicitly chosen this session, keyed by
// username so the UI switches immediately while the PATCH persists and so
// logging out and back in as a different user follows the server preference
// again
interface SessionOverride {
  user: string;
  lang: Lang;
}

export function LangProvider({ children }: { children: React.ReactNode }) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/auth/me") });
  const qc = useQueryClient();
  const [override, setOverride] = useState<SessionOverride | null>(null);

  const serverLang: Lang | null =
    me.data?.language === "id" ? "id" : me.data?.language === "en" ? "en" : null;
  const effective: Lang =
    override && me.data && override.user === me.data.username
      ? override.lang
      : (serverLang ?? "en");

  const patch = useMutation({
    mutationFn: (l: Lang) =>
      api("/auth/me", { method: "PATCH", body: JSON.stringify({ language: l }) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
  const { mutate: patchLang } = patch;

  const ctx = useMemo<LangCtx>(() => ({
    lang: effective,
    setLang: (l) => {
      const user = me.data?.username;
      if (user) setOverride({ user, lang: l });
      patchLang(l);
    },
  }), [effective, me.data?.username, patchLang]);

  return <Ctx.Provider value={ctx}>{children}</Ctx.Provider>;
}

export function useT() {
  const { lang } = useContext(Ctx);
  const dict = DICTS[lang];
  return (key: string, vars?: Record<string, string>): string => {
    let s = dict[key] ?? EN[key] ?? key;
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, v);
    return s;
  };
}

export function useI18nLang() {
  const { lang, setLang } = useContext(Ctx);
  return { lang, setLang };
}
