// export.tsx — connections export button: downloads every managed
// connection as YAML or JSON with secret values masked (******).
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Braces, ChevronDown, Download, FileText, Loader2 } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export function ExportButton({ count }: { count: number }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)

  async function download(format: "yaml" | "json") {
    setBusy(true)
    try {
      const res = await api.exportConns(format)
      const blob = new Blob([res.body], { type: format === "yaml" ? "text/yaml" : "application/json" })
      const a = document.createElement("a")
      a.href = URL.createObjectURL(blob)
      a.download = res.name
      a.click()
      URL.revokeObjectURL(a.href)
      toast.success(t("connections.exportToast", { count: res.count }))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          disabled={count === 0 || busy}
          title={count === 0 ? t("connections.exportEmpty") : t("connections.exportTooltip")}
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Download className="size-4" />}
          {t("connections.exportBtn")}
          <ChevronDown className="size-3.5 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => download("yaml")}>
          <FileText className="size-4" /> {t("connections.exportYaml")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => download("json")}>
          <Braces className="size-4" /> {t("connections.exportJson")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
