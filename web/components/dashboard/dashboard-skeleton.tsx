import { cn } from "@/lib/utils";

/**
 * Placeholder layout while /v1/me/bootstrap and /v1/me/status settle.
 * Matches the real dashboard grid so the switch to content doesn’t jump.
 */
export function DashboardSkeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "animate-in fade-in duration-300 p-6 lg:p-8 space-y-6",
        className,
      )}
    >
      <section className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="rounded-xl border border-border/60 bg-card px-5 py-4 space-y-3"
          >
            <div className="flex justify-between">
              <div className="h-3 w-24 bg-muted rounded animate-pulse" />
              <div className="size-3.5 bg-muted rounded animate-pulse" />
            </div>
            <div className="h-8 w-16 bg-muted/80 rounded-md animate-pulse" />
            <div className="h-2.5 w-14 bg-muted/60 rounded animate-pulse" />
          </div>
        ))}
      </section>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 rounded-xl border border-border/60 bg-card overflow-hidden">
          <div className="flex items-center justify-between px-5 py-3 border-b border-border/60">
            <div className="h-3.5 w-32 bg-muted rounded animate-pulse" />
            <div className="h-3 w-16 bg-muted/70 rounded animate-pulse" />
          </div>
          <div className="p-5 space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className="flex gap-3 py-2 border-b border-border/40 last:border-0"
              >
                <div className="size-2 rounded-full bg-muted mt-1.5 shrink-0 animate-pulse" />
                <div className="flex-1 space-y-2">
                  <div className="h-3 w-[55%] max-w-xs bg-muted rounded animate-pulse" />
                  <div className="h-2.5 w-48 bg-muted/60 rounded animate-pulse" />
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="space-y-6">
          <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
            <div className="flex justify-between">
              <div className="h-3 w-28 bg-muted rounded animate-pulse" />
              <div className="h-4 w-10 bg-muted/80 rounded animate-pulse tabular-nums" />
            </div>
            <div className="h-3 w-full bg-muted/50 rounded-full overflow-hidden">
              <div className="h-full w-[28%] bg-muted animate-pulse" />
            </div>
            <div className="h-12 w-full bg-muted/40 rounded animate-pulse" />
          </div>
          <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
            <div className="flex justify-between">
              <div className="h-3 w-24 bg-muted rounded animate-pulse" />
              <div className="h-3 w-12 bg-muted/70 rounded animate-pulse" />
            </div>
            <div className="grid grid-cols-10 gap-1">
              {Array.from({ length: 40 }).map((_, i) => (
                <div key={i} className="aspect-square rounded-[3px] bg-muted/60 animate-pulse" />
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
