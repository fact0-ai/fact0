"use client";

import { ExternalLink, MapPin, X } from "lucide-react";
import { createPortal } from "react-dom";
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

export interface HelpConcept {
  title: string;
  body: string;
}

export interface PageHelpContent {
  title: string;
  tagline: string;
  icon: React.ReactNode;
  concepts: HelpConcept[];
  docHref: string;
  onTourStart?: () => void;
}

interface PageHelpSheetProps extends PageHelpContent {
  isOpen: boolean;
  onClose: () => void;
}

export function PageHelpSheet({
  isOpen,
  onClose,
  title,
  tagline,
  icon,
  concepts,
  docHref,
  onTourStart,
}: PageHelpSheetProps) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMounted(true); // SSR guard: createPortal requires document.body (client-only)
  }, []);

  // Close on Escape key
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [isOpen, onClose]);

  if (!mounted) return null;

  return createPortal(
    <>
      {/* Backdrop */}
      <div
        aria-hidden
        onClick={onClose}
        className={cn(
          "fixed inset-0 z-40 bg-black/50 backdrop-blur-xs transition-opacity duration-300",
          isOpen ? "opacity-100 pointer-events-auto" : "opacity-0 pointer-events-none",
        )}
      />

      {/* Panel */}
      <div
        role="dialog"
        aria-modal
        aria-label={`${title} help`}
        className={cn(
          "fixed inset-y-0 right-0 z-50 w-80 md:w-96 bg-card border-l border-border/60 flex flex-col shadow-2xl transition-transform duration-300 ease-out",
          isOpen ? "translate-x-0" : "translate-x-full",
        )}
      >
        {/* Header */}
        <div className="flex items-center gap-3 px-5 py-4 border-b border-border/60 shrink-0">
          <div className="size-8 rounded-lg bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center text-indigo-500 shrink-0">
            {icon}
          </div>
          <span className="flex-1 text-sm font-semibold text-foreground dark:text-white truncate">
            {title}
          </span>
          <button
            type="button"
            onClick={onClose}
            className="size-7 flex items-center justify-center rounded-md text-zinc-500 hover:text-foreground hover:bg-muted/60 transition-colors shrink-0"
          >
            <X className="size-4" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-5 space-y-5">
          <p className="text-xs text-zinc-500 leading-relaxed">{tagline}</p>

          <div className="h-px bg-border/60" />

          <div className="space-y-4">
            {concepts.map((c, i) => (
              <div key={i} className="space-y-1">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-foreground/80">{c.title}</p>
                <p className="text-xs text-zinc-500 leading-relaxed">{c.body}</p>
              </div>
            ))}
          </div>
        </div>

        {/* Footer */}
        <div className="px-5 py-4 border-t border-border/60 space-y-2.5 shrink-0">
          {onTourStart && (
            <button
              type="button"
              onClick={() => { onTourStart(); onClose(); }}
              className="w-full flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold transition-colors"
            >
              <MapPin className="size-3.5" />
              Take a guided tour
            </button>
          )}
          <a
            href={docHref}
            target="_blank"
            rel="noopener noreferrer"
            className="w-full flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg border border-border/60 hover:bg-muted/40 text-xs font-medium text-zinc-500 hover:text-foreground transition-colors"
          >
            <ExternalLink className="size-3.5" />
            View documentation
          </a>
        </div>
      </div>
    </>,
    document.body,
  );
}
