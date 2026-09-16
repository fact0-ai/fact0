import Link from "next/link";
import { Home, LayoutDashboard, Search } from "lucide-react";

import { BrandLogo } from "@/components/brand/brand-logo";

export default function NotFound() {
  if (process.env.NEXT_PUBLIC_MARKETING_ONLY === "1") {
    return (
      <main className="mx-auto flex min-h-screen max-w-3xl flex-col items-start justify-center gap-6 px-6 py-20">
        <BrandLogo />
        <p className="text-sm text-muted-foreground">404</p>
        <h1 className="text-4xl font-semibold tracking-tight">Page not found</h1>
        <p className="text-muted-foreground">
          This page is unavailable. Return to Fact0 to find the current guides
          and installation instructions.
        </p>
        <Link className="rounded-lg border px-5 py-3 text-sm" href="/">
          Back to Fact0
        </Link>
      </main>
    );
  }
  return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center relative overflow-hidden">
      {/* Background gradient */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top_right,var(--tw-gradient-stops))] from-indigo-500/8 via-transparent to-transparent pointer-events-none" />
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_bottom_left,var(--tw-gradient-stops))] from-violet-500/5 via-transparent to-transparent pointer-events-none" />

      {/* Subtle grid */}
      <div
        className="absolute inset-0 opacity-[0.02] dark:opacity-[0.04] pointer-events-none"
        style={{
          backgroundImage:
            "linear-gradient(to right, hsl(var(--border)) 1px, transparent 1px), linear-gradient(to bottom, hsl(var(--border)) 1px, transparent 1px)",
          backgroundSize: "60px 60px",
        }}
      />

      <div className="relative z-10 flex flex-col items-center text-center max-w-lg mx-auto px-6">
        {/* 404 numeral */}
        <div className="relative mb-6 select-none">
          <span
            className="text-[160px] font-black tracking-tighter leading-none"
            style={{
              background:
                "linear-gradient(135deg, #6366f1 0%, #8b5cf6 40%, #a78bfa 70%, #c4b5fd 100%)",
              WebkitBackgroundClip: "text",
              WebkitTextFillColor: "transparent",
              backgroundClip: "text",
              filter: "drop-shadow(0 0 60px rgba(99,102,241,0.25))",
            }}
          >
            404
          </span>
          {/* Glow behind the number */}
          <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
            <div className="size-48 rounded-full bg-indigo-500/10 blur-3xl" />
          </div>
        </div>

        {/* Overline */}
        <p className="text-[10px] font-black tracking-[0.4em] text-indigo-500 uppercase mb-3">
          Page not found
        </p>

        {/* Title */}
        <h1 className="text-2xl font-black tracking-tighter text-foreground dark:text-white mb-3">
          This route doesn&apos;t exist
        </h1>

        {/* Description */}
        <p className="text-sm text-zinc-500 dark:text-zinc-400 leading-relaxed mb-10 max-w-sm">
          The page you&apos;re looking for was moved, deleted, or never existed.
          Head back to the dashboard or the landing page.
        </p>

        {/* CTAs */}
        <div className="flex flex-col sm:flex-row items-center gap-3 w-full sm:w-auto">
          <Link
            href="/dashboard"
            className="w-full sm:w-auto flex items-center justify-center gap-2.5 px-6 py-3 rounded-2xl bg-indigo-600 text-white text-xs font-black uppercase tracking-widest shadow-[0_0_20px_rgba(79,70,229,0.35)] hover:shadow-[0_0_30px_rgba(79,70,229,0.5)] hover:scale-105 transition-all"
          >
            <LayoutDashboard className="size-3.5" />
            Go to Dashboard
          </Link>

          <Link
            href="/"
            className="w-full sm:w-auto flex items-center justify-center gap-2.5 px-6 py-3 rounded-2xl bg-card border border-border text-xs font-black uppercase tracking-widest text-zinc-500 hover:text-foreground hover:border-border/80 hover:bg-muted transition-all"
          >
            <Home className="size-3.5" />
            Back to Home
          </Link>
        </div>

        {/* Helpful links */}
        <div className="mt-10 pt-8 border-t border-border/50 w-full space-y-2">
          <p className="text-[9px] font-bold text-zinc-500 uppercase tracking-widest mb-4">
            Quick navigation
          </p>
          <div className="grid grid-cols-2 gap-2">
            {[
              { label: "Executions", href: "/dashboard/executions" },
              { label: "Audit Logs", href: "/dashboard/audit" },
              { label: "API Keys", href: "/dashboard/keys" },
              { label: "Settings", href: "/dashboard/settings" },
            ].map(({ label, href }) => (
              <Link
                key={href}
                href={href}
                className="flex items-center gap-2 px-4 py-2.5 rounded-xl bg-muted/50 border border-border/50 text-[10px] font-bold uppercase tracking-widest text-zinc-500 hover:text-foreground hover:border-border hover:bg-muted transition-all"
              >
                <Search className="size-3 shrink-0" />
                {label}
              </Link>
            ))}
          </div>
        </div>

        {/* Brand footer */}
        <div className="mt-10 flex items-center gap-2">
          <BrandLogo href={null} size="xs" className="opacity-50" />
          <span className="size-1 rounded-full bg-zinc-300 dark:bg-zinc-700" />
          <span className="text-[9px] font-bold text-zinc-400 dark:text-zinc-700 uppercase tracking-widest">
            Experimental self-hosted audit history
          </span>
        </div>
      </div>
    </div>
  );
}
