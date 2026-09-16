"use client";

import { ExternalLink, MapPin } from "lucide-react";
import { cn } from "@/lib/utils";

interface DocLink {
  label: string;
  href: string;
}

interface PageGetStartedProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  docLinks?: DocLink[];
  onTourStart?: () => void;
  className?: string;
}

export function PageGetStarted({
  icon,
  title,
  description,
  docLinks,
  onTourStart,
  className,
}: PageGetStartedProps) {
  return (
    <div
      className={cn(
        "rounded-xl border border-border/60 bg-card px-6 py-16 flex flex-col items-center text-center gap-4",
        className,
      )}
    >
      <div className="size-10 rounded-lg bg-muted border border-border flex items-center justify-center">
        {icon}
      </div>
      <div className="space-y-1.5">
        <p className="text-sm font-medium text-foreground/90">{title}</p>
        <p className="text-xs text-zinc-500 max-w-sm leading-relaxed">{description}</p>
      </div>
      {(docLinks && docLinks.length > 0) || onTourStart ? (
        <div className="flex flex-wrap items-center justify-center gap-3 mt-1">
          {docLinks?.map((link) => (
            <a
              key={link.href}
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs font-medium text-indigo-600 dark:text-indigo-400 hover:underline underline-offset-2 transition-colors"
            >
              <ExternalLink className="size-3" />
              {link.label}
            </a>
          ))}
          {onTourStart && (
            <button
              type="button"
              onClick={onTourStart}
              className="inline-flex items-center gap-1 text-xs font-medium text-zinc-500 hover:text-foreground transition-colors"
            >
              <MapPin className="size-3" />
              Take a tour
            </button>
          )}
        </div>
      ) : null}
    </div>
  );
}
