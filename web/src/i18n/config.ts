import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import enUS from "./locales/en-US.json";
import zhCN from "./locales/zh-CN.json";
import zhTW from "./locales/zh-TW.json";

// The three supported languages. Labels use each language's native name (never
// translated); the language switcher displays them verbatim.
export const LANGS = [
  { code: "zh-CN", label: "简体中文" },
  { code: "zh-TW", label: "繁體中文" },
  { code: "en-US", label: "English" },
] as const;

export type LangCode = (typeof LANGS)[number]["code"];

export const DEFAULT_LANG: LangCode = "zh-CN";
export const LANG_STORAGE_KEY = "antifox-lang";

export function isLangCode(v: string | null | undefined): v is LangCode {
  return !!v && LANGS.some((l) => l.code === v);
}

// Bundled resources (no async loading) - static-export friendly, no Suspense, consistent SSR/first paint.
if (!i18n.isInitialized) {
  void i18n.use(initReactI18next).init({
    resources: {
      "zh-CN": { translation: zhCN },
      "zh-TW": { translation: zhTW },
      "en-US": { translation: enUS },
    },
    lng: DEFAULT_LANG,
    fallbackLng: DEFAULT_LANG,
    interpolation: { escapeValue: false },
    returnNull: false,
  });
}

export default i18n;
