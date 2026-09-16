import { cn } from "@/lib/utils";
import { LucideIcon } from "lucide-react";

interface Props {
  title: string;
  value: string | number;
  /** Small secondary subtitle (units, e.g. "events"). */
  unit?: string;
  /** Period-over-period delta string, e.g. "+12%". */
  change?: string;
  trend?: "up" | "down" | "neutral";
  icon: LucideIcon;
  /** When true the title gets a small pulsing dot indicating live data. */
  pulse?: boolean;
  loading?: boolean;
  /** Optional class override for the value (e.g. long tenant ids). */
  valueClassName?: string;
  /** Shown on hover when value is truncated. */
  valueTitle?: string;
  /** Optional secondary subtitle string at the bottom. */
  subtitle?: string;
}

/**
 * Flat KPI tile. One quiet border, no glow, no shadow - designed to
 * sit in a 4-up strip across the top of the dashboard, matching the
 * Agnost reference: small labelled header + large plain number +
 * optional unit. Icons are subtle (top-right, muted) so the eye
 * lands on the value first.
 */
export function MetricCard({
  title,
  value,
  unit,
  change,
  trend = "neutral",
  icon: Icon,
  pulse,
  loading,
  valueClassName,
  valueTitle,
  subtitle,
}: Props) {
  return (
    <div className="min-w-0 bg-transparent px-6 py-5 overflow-hidden group">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          {pulse && (
            <span className="size-1.5 rounded-full bg-zinc-800 dark:bg-zinc-200 animate-pulse shrink-0" />
          )}
          <h3 className="text-[11px] font-semibold tracking-wide text-zinc-500 uppercase truncate">
            {title}
          </h3>
        </div>
        <Icon className="size-3.5 text-zinc-400 shrink-0" />
      </div>
      <div className="mt-3 flex min-w-0 items-baseline gap-2">
        <span
          title={valueTitle}
          className={cn(
            "min-w-0 text-3xl font-medium tracking-tight tabular-nums text-foreground dark:text-white",
            valueClassName ?? "truncate",
            loading && "opacity-40 animate-pulse",
          )}
        >
          {value}
        </span>
        {unit && (
          <span className="text-xs text-zinc-500">{unit}</span>
        )}
        {change && (
          <span
            className={cn(
              "ml-auto text-[11px] font-medium tabular-nums",
              trend === "up"
                ? "text-emerald-600 dark:text-emerald-400"
                : trend === "down"
                  ? "text-red-600 dark:text-red-400"
                  : "text-zinc-500",
            )}
          >
            {change}
          </span>
        )}
      </div>
      {subtitle && (
        <p className="mt-2 text-[10px] font-medium text-zinc-400 dark:text-zinc-500">
          {subtitle}
        </p>
      )}
    </div>
  );
}
