// src/i18n/index.ts — bilingual i18n (English default, Simplified Chinese supported)
import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import zhCN from "./zh-CN.json"
import en from "./en.json"

export type Lang = "zh-CN" | "en"

const saved = localStorage.getItem("ui.language")
const initial: Lang = saved === "zh-CN" || saved === "en" ? saved : "en"

i18n.use(initReactI18next).init({
  resources: {
    "zh-CN": { translation: zhCN },
    en: { translation: en },
  },
  lng: initial,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
})

// keep <html lang> in sync with the active language on init
document.documentElement.lang = initial

export function setLang(lang: Lang) {
  localStorage.setItem("ui.language", lang)
  i18n.changeLanguage(lang)
  document.documentElement.lang = lang
}

export default i18n
