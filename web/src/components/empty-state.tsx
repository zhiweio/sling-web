// 空状态（引导插画 + CTA）
import { Button } from "@/components/ui/button"

export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
  onAction,
}: {
  icon: React.ComponentType<{ className?: string }>
  title: string
  body?: string
  action?: string
  onAction?: () => void
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed px-6 py-16 text-center">
      <div className="mb-4 flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary">
        <Icon className="size-7" />
      </div>
      <h3 className="text-base font-semibold">{title}</h3>
      {body && <p className="mt-1.5 max-w-md text-sm leading-relaxed text-muted-foreground">{body}</p>}
      {action && onAction && (
        <Button className="mt-5" onClick={onAction}>
          {action}
        </Button>
      )}
    </div>
  )
}
