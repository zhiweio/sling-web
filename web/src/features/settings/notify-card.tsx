// Settings · notifications: success/failure switches + global notification channels (WeCom/Feishu/DingTalk/Slack/generic)
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Pencil, Plus, Send, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, type ChannelInput } from "@/lib/api"
import type { NotifyChannel } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

const PROVIDERS = ["wecom", "feishu", "dingtalk", "slack", "generic"]
const EVENTS = ["run_success", "run_failed", "run_cancelled", "paused", "system"]

export function NotifyCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: settings } = useQuery({ queryKey: ["settings"], queryFn: api.getSettings })
  const { data: channels } = useQuery({ queryKey: ["notify-channels"], queryFn: api.notifyChannels })

  const putSetting = useMutation({
    mutationFn: (kv: Record<string, string>) => api.putSettings(kv),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
    onError: (e) => toast.error(e.message),
  })

  const [editing, setEditing] = useState<NotifyChannel | null>(null)
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<NotifyChannel | null>(null)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("settings.notify")}</CardTitle>
        <CardDescription>{t("settings.notifyDesc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Event switches */}
        <div className="space-y-3">
          <SettingSwitch
            label={t("settings.notifyOnSuccess")}
            checked={settings?.["notify.on_success"] === "true"}
            onCheckedChange={(v) => putSetting.mutate({ "notify.on_success": String(v) })}
          />
          <SettingSwitch
            label={t("settings.notifyOnFailure")}
            checked={settings?.["notify.on_failure"] !== "false"}
            onCheckedChange={(v) => putSetting.mutate({ "notify.on_failure": String(v) })}
          />
        </div>

        {/* Channel list */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">{t("settings.notifyChannels")}</span>
            <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
              <Plus className="size-4" /> {t("settings.addChannel")}
            </Button>
          </div>
          {(channels?.channels ?? []).length === 0 ? (
            <div className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t("settings.noChannels")}
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border">
              {(channels?.channels ?? []).map((c) => (
                <ChannelRow key={c.id} channel={c} onEdit={() => setEditing(c)} onDelete={() => setDeleting(c)} />
              ))}
            </div>
          )}
        </div>
      </CardContent>

      <ChannelDialog
        open={creating || editing !== null}
        channel={editing}
        onClose={() => { setCreating(false); setEditing(null) }}
      />

      <AlertDialog open={deleting !== null} onOpenChange={(v) => !v && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("common.confirmDelete")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("settings.deleteChannelDesc", { name: deleting?.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.close")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleting) {
                  api.deleteNotifyChannel(deleting.id)
                    .then(() => qc.invalidateQueries({ queryKey: ["notify-channels"] }))
                    .catch((e) => toast.error(e.message))
                    .finally(() => setDeleting(null))
                }
              }}
            >
              {t("app.actions.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

function SettingSwitch({ label, checked, onCheckedChange }: {
  label: string
  checked: boolean
  onCheckedChange: (v: boolean) => void
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between text-sm">
      <span>{label}</span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </label>
  )
}

function ChannelRow({ channel, onEdit, onDelete }: {
  channel: NotifyChannel
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [testing, setTesting] = useState(false)

  const toggle = useMutation({
    mutationFn: (enabled: boolean) =>
      api.updateNotifyChannel(channel.id, {
        name: channel.name, provider: channel.provider, url: channel.url, enabled,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notify-channels"] }),
    onError: (e) => toast.error(e.message),
  })

  const test = async () => {
    setTesting(true)
    try {
      const res = await api.testNotifyChannel(channel.id)
      if (res.ok) toast.success(t("settings.testOk"))
      else toast.error(t("settings.testFail", { error: res.error ?? "" }))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-3 border-b px-3 py-2.5 last:border-b-0">
      <div className="min-w-40 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{channel.name}</span>
          <Badge variant="secondary" className="text-[10px]">{t(`notify.provider.${channel.provider}`)}</Badge>
          {channel.has_secret && <span className="text-[10px] text-muted-foreground">🔑</span>}
        </div>
        <div className="truncate font-mono text-[11px] text-muted-foreground">{channel.url}</div>
      </div>
      <span className="hidden text-xs text-muted-foreground md:inline">
        {channel.events.length === 0 ? t("notify.eventsAll") : channel.events.map((e) => t(`notify.type.${e}`, e)).join(" / ")}
      </span>
      <Switch checked={channel.enabled} onCheckedChange={(v) => toggle.mutate(v)} />
      <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={test} disabled={testing}>
        <Send className="size-3.5" /> {testing ? "…" : t("settings.testChannel")}
      </Button>
      <Button variant="ghost" size="icon" className="size-8" onClick={onEdit} title={t("app.actions.edit")}>
        <Pencil className="size-3.5" />
      </Button>
      <Button variant="ghost" size="icon" className="size-8 text-destructive" onClick={onDelete} title={t("app.actions.delete")}>
        <Trash2 className="size-3.5" />
      </Button>
    </div>
  )
}

function ChannelDialog({ open, channel, onClose }: {
  open: boolean
  channel: NotifyChannel | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form, setForm] = useState<ChannelInput>({ name: "", provider: "wecom", url: "", secret: "", events: [] })

  useEffect(() => {
    if (!open) return
    if (channel) {
      setForm({ name: channel.name, provider: channel.provider, url: channel.url, secret: "", events: channel.events })
    } else {
      setForm({ name: "", provider: "wecom", url: "", secret: "", events: [] })
    }
  }, [open, channel])

  const save = useMutation({
    mutationFn: () =>
      channel ? api.updateNotifyChannel(channel.id, form) : api.createNotifyChannel({ ...form, enabled: true }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["notify-channels"] })
      toast.success(t("common.saved"))
      onClose()
    },
    onError: (e) => toast.error(e.message),
  })

  const toggleEvent = (ev: string) => {
    setForm((f) => ({
      ...f,
      events: f.events?.includes(ev) ? f.events.filter((x) => x !== ev) : [...(f.events ?? []), ev],
    }))
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{channel ? t("settings.editChannel") : t("settings.addChannel")}</DialogTitle>
          <DialogDescription>{t("settings.channelDesc")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>{t("settings.channelName")}</Label>
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="ops-wecom" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("settings.channelProvider")}</Label>
              <Select value={form.provider} onValueChange={(v) => setForm({ ...form, provider: v })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {PROVIDERS.map((p) => (
                    <SelectItem key={p} value={p}>{t(`notify.provider.${p}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label>Webhook URL</Label>
            <Input
              value={form.url}
              onChange={(e) => setForm({ ...form, url: e.target.value })}
              placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
              className="font-mono text-xs"
            />
            {channel && <p className="text-[11px] text-muted-foreground">{t("settings.channelURLEditHint")}</p>}
          </div>
          <div className="space-y-1.5">
            <Label>{t("settings.channelSecret")}</Label>
            <Input
              type="password"
              value={form.secret}
              onChange={(e) => setForm({ ...form, secret: e.target.value })}
              placeholder={channel?.has_secret ? t("settings.secretKeepHint") : t("settings.secretPlaceholder")}
            />
            <p className="text-[11px] text-muted-foreground">{t("settings.secretHint")}</p>
          </div>
          <div className="space-y-1.5">
            <Label>{t("settings.channelEvents")}</Label>
            <div className="flex flex-wrap gap-3">
              {EVENTS.map((ev) => (
                <label key={ev} className="flex cursor-pointer items-center gap-1.5 text-sm">
                  <Checkbox checked={form.events?.includes(ev) ?? false} onCheckedChange={() => toggleEvent(ev)} />
                  {t(`notify.type.${ev}`)}
                </label>
              ))}
            </div>
            <p className="text-[11px] text-muted-foreground">{t("settings.channelEventsHint")}</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("app.actions.cancelEdit")}</Button>
          <Button
            onClick={() => save.mutate()}
            disabled={save.isPending || !form.name.trim() || !form.url.trim()}
          >
            {t("app.actions.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
