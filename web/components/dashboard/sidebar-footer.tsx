"use client";

import { LogOut, MoreHorizontal } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { authClient } from "@/lib/auth-client";
import { signInHref } from "@/lib/app-origin";
import { invalidateTokenCache } from "@/lib/use-me";
import { cn } from "@/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

function displayName(user: { name?: string | null; email: string } | null | undefined): string {
  if (!user) return "";
  if (user.name && user.name.trim()) return user.name;
  return user.email.split("@")[0] || "Account";
}

function initials(name: string, email: string): string {
  const src = (name || email).trim();
  if (!src) return "?";
  const parts = src.split(/[\s@.]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "") + (parts[1]?.[0] ?? "")).toUpperCase() || src[0].toUpperCase();
}

interface SidebarFooterProps {
  demo?: boolean;
  isCollapsed?: boolean;
}

export function SidebarFooter({ demo, isCollapsed }: SidebarFooterProps) {
  const { data: session, isPending } = authClient.useSession();
  const [open, setOpen] = useState(false);
  const popRef = useRef<HTMLDivElement>(null);

  const name = demo ? "Jordan Lee" : displayName(session?.user);
  const email = demo ? "jordan@acme-finance.com" : (session?.user?.email ?? "");
  const loading = demo ? false : isPending;

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (!popRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const onSignOut = async () => {
    if (demo) {
      window.location.assign(signInHref());
      return;
    }
    invalidateTokenCache();
    await authClient.signOut();
    window.location.assign(signInHref());
  };

  const buttonContent = (
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "w-full flex items-center rounded-lg hover:bg-muted/60 transition-colors text-left relative group",
          isCollapsed ? "justify-center px-0 py-2" : "gap-2.5 px-2 py-2"
        )}
      >
        <div className="size-8 rounded-lg bg-zinc-100 dark:bg-zinc-800 border border-border flex items-center justify-center text-foreground font-bold text-xs shrink-0">
          {loading ? "·" : initials(name, email)}
        </div>
        {!isCollapsed && (
          <div className="flex-1 min-w-0">
            {loading ? (
              <div className="space-y-1">
                <div className="h-3 w-20 bg-muted rounded-full animate-pulse" />
                <div className="h-2.5 w-28 bg-muted rounded-full animate-pulse" />
              </div>
            ) : (
              <>
                <p className="text-xs font-bold text-foreground dark:text-white truncate leading-tight">
                  {name}
                </p>
                <p className="text-[10px] text-zinc-500 truncate font-mono leading-snug mt-0.5">
                  {email}
                </p>
              </>
            )}
          </div>
        )}
        {!isCollapsed && <MoreHorizontal className="size-3.5 text-zinc-400 shrink-0" />}
      </button>
  );

  return (
    <div className="shrink-0 border-t border-border/50 p-3 relative" ref={popRef}>
      {isCollapsed ? (
        <Tooltip>
          <TooltipTrigger asChild>{buttonContent}</TooltipTrigger>
          <TooltipContent side="right" sideOffset={12}>Profile</TooltipContent>
        </Tooltip>
      ) : buttonContent}
      {open && (
        <div className={cn(
          "absolute bottom-full mb-2 z-50 rounded-xl border border-border bg-popover shadow-2xl overflow-hidden animate-in fade-in slide-in-from-bottom-1 duration-150",
          isCollapsed ? "left-full ml-2" : "left-3 right-3"
        )}>
          <button
            type="button"
            onClick={onSignOut}
            className="w-full flex items-center gap-2.5 px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/60 transition-colors"
          >
            <LogOut className="size-3.5 text-red-500" />
            {demo ? "Exit demo" : "Sign out"}
          </button>
        </div>
      )}
    </div>
  );
}
