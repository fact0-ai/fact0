"use client";

import Link from "next/link";
import { ShieldOff } from "lucide-react";

import { useCurrentMember } from "@/lib/use-current-member";

interface AdminGuardProps {
  children: React.ReactNode;
}

/**
 * Renders children only for owner/admin members of the active org.
 * Non-admins see a clear "Admin only" message.
 * Shows a skeleton while the session/active-org are still loading.
 */
export function AdminGuard({ children }: AdminGuardProps) {
  const { isAdmin, isLoading } = useCurrentMember();

  if (isLoading) {
    return (
      <div className="animate-in fade-in duration-500 space-y-10">
        <div className="space-y-2">
          <div className="h-3 w-24 bg-muted rounded-full animate-pulse" />
          <div className="h-10 w-48 bg-muted rounded-2xl animate-pulse" />
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-36 rounded-3xl bg-muted animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  if (!isAdmin) {
    return (
      <div className="animate-in fade-in slide-in-from-bottom-4 duration-700 ease-out h-[calc(100vh-80px)] flex flex-col items-center justify-center gap-6">
        <div className="size-16 rounded-3xl bg-destructive/8 border border-destructive/20 flex items-center justify-center">
          <ShieldOff className="size-8 text-destructive/70" />
        </div>
        <div className="text-center space-y-2 max-w-sm">
          <p className="text-[10px] font-black uppercase tracking-[0.3em] text-destructive/70">
            Access Restricted
          </p>
          <h2 className="text-2xl font-black tracking-tighter text-foreground dark:text-white">
            Admins only
          </h2>
          <p className="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
            This page is restricted to organization administrators. Contact your org admin if you need access.
          </p>
        </div>
        <Link
          href="/dashboard"
          className="px-5 py-2.5 rounded-xl bg-muted border border-border text-xs font-black uppercase tracking-widest text-zinc-500 hover:text-foreground hover:border-border/80 transition-all"
        >
          ← Back to Dashboard
        </Link>
      </div>
    );
  }

  return <>{children}</>;
}
