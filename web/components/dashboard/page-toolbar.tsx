"use client";

import { Clock, Filter, HelpCircle, MapPin, RefreshCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import { PageHelpSheet } from "@/components/dashboard/page-help-sheet";
import type { PageHelpContent } from "@/components/dashboard/page-help-sheet";
import { ThemeToggle } from "@/components/theme-toggle";
import { cn } from "@/lib/utils";

export type TimeRange = "1h" | "24h" | "7d" | "all";

const TIME_RANGE_LABEL: Record<TimeRange, string> = {
  "1h": "1H",
  "24h": "24H",
  "7d": "7D",
  all: "ALL",
};

const TIME_RANGE_CYCLE: TimeRange[] = ["1h", "24h", "7d", "all"];

interface PageToolbarProps {
  title: string;
  showFilters?: boolean;
  children?: React.ReactNode;
  /** Active time window label on the clock button. */
  timeRange?: TimeRange;
  onTimeRangeChange?: (range: TimeRange) => void;
  /** Whether the filter drawer/row is open. */
  filtersOpen?: boolean;
  onFiltersToggle?: () => void;
  /** Override refresh - defaults to router.refresh(). */
  onRefresh?: () => void;
  /** When provided, renders a "Take a tour" button that calls this handler. */
  onTourStart?: () => void;
  /** When provided, renders a HelpCircle button that opens a contextual help sheet. */
  helpContent?: PageHelpContent;
}

export function PageToolbar({
  title,
  showFilters = true,
  children,
  timeRange = "1h",
  onTimeRangeChange,
  filtersOpen = false,
  onFiltersToggle,
  onRefresh,
  onTourStart,
  helpContent,
}: PageToolbarProps) {
  const router = useRouter();
  const [refreshing, startTransition] = useTransition();
  const [isHelpOpen, setIsHelpOpen] = useState(false);

  const onRefreshClick = () => {
    if (onRefresh) {
      onRefresh();
      return;
    }
    startTransition(() => {
      router.refresh();
    });
  };

  const cycleTimeRange = () => {
    if (!onTimeRangeChange) return;
    const idx = TIME_RANGE_CYCLE.indexOf(timeRange);
    const next = TIME_RANGE_CYCLE[(idx + 1) % TIME_RANGE_CYCLE.length];
    onTimeRangeChange(next);
  };

  return (
    <>
    <header className="sticky top-0 z-20 border-b border-border/70 bg-background/95 backdrop-blur-xl supports-backdrop-filter:backdrop-saturate-150">
      <div className="px-6 lg:px-8 py-4">
        <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
          <div className="min-w-0 flex-1 space-y-2">
            <h1 className="font-inter text-[1.0625rem] font-bold tracking-tight text-foreground leading-tight dark:text-white truncate">
              {title}
            </h1>
            {children != null ? (
              <div className="flex flex-wrap items-center gap-2">{children}</div>
            ) : null}
          </div>

          <div className="flex items-center shrink-0 gap-2 pt-0.5">
            {showFilters && (
              <div className="flex items-center rounded-lg border border-border/70 bg-muted/35 p-0.5 shadow-2xs">
                <ToolbarIconButton
                  icon={Clock}
                  label={TIME_RANGE_LABEL[timeRange]}
                  title="Cycle time range (1H → 24H → 7D → All)"
                  onClick={cycleTimeRange}
                  active={timeRange !== "all"}
                  disabled={!onTimeRangeChange}
                />
                <span className="mx-0.5 h-4 w-px bg-border/80" aria-hidden />
                <ToolbarIconButton
                  icon={Filter}
                  label="Filters"
                  title="Toggle filters"
                  onClick={onFiltersToggle}
                  active={filtersOpen}
                  disabled={!onFiltersToggle}
                />
                <span className="mx-0.5 h-4 w-px bg-border/80" aria-hidden />
                <ToolbarIconButton
                  icon={RefreshCw}
                  label="Refresh"
                  title="Refresh"
                  onClick={onRefreshClick}
                  spinning={refreshing}
                />
              </div>
            )}
            <ThemeToggle />
            {onTourStart && (
              <button
                type="button"
                onClick={onTourStart}
                title="Take a tour"
                className="flex items-center gap-1.5 rounded-lg border border-border/70 bg-muted/35 px-2.5 py-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground shadow-2xs transition-colors hover:bg-background/85 hover:text-foreground"
              >
                <MapPin className="size-3 shrink-0" />
                <span>Tour</span>
              </button>
            )}
            {helpContent && (
              <button
                type="button"
                onClick={() => setIsHelpOpen(true)}
                title="Learn about this page"
                className="flex items-center gap-1.5 rounded-lg border border-border/70 bg-muted/35 px-2.5 py-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground shadow-2xs transition-colors hover:bg-background/85 hover:text-foreground"
              >
                <HelpCircle className="size-3 shrink-0" />
                <span>Help</span>
              </button>
            )}
          </div>
        </div>
      </div>
    </header>
    {helpContent && (
      <PageHelpSheet
        isOpen={isHelpOpen}
        onClose={() => setIsHelpOpen(false)}
        {...helpContent}
      />
    )}
    </>
  );
}

function ToolbarIconButton({
  icon: Icon,
  label,
  title,
  onClick,
  spinning,
  active,
  disabled,
}: {
  icon: typeof Clock;
  label: string;
  title: string;
  onClick?: () => void;
  spinning?: boolean;
  active?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] font-medium uppercase tracking-wide transition-colors",
        "disabled:cursor-not-allowed disabled:opacity-40",
        active
          ? "bg-background text-foreground shadow-sm"
          : "text-muted-foreground hover:bg-background/85 hover:text-foreground",
      )}
    >
      <Icon className={cn("size-3 shrink-0", spinning && "animate-spin")} />
      <span>{label}</span>
    </button>
  );
}
