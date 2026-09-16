"use client";

import { createContext, useContext, useMemo } from "react";

const DemoModeContext = createContext(false);

export function DemoModeProvider({ children }: { children: React.ReactNode }) {
  return <DemoModeContext.Provider value={true}>{children}</DemoModeContext.Provider>;
}

export function useDemoMode(): boolean {
  return useContext(DemoModeContext);
}

/** `/dashboard` in prod, `/demo/dashboard` in incognito demo. */
export function useDashboardBasePath(): string {
  const demo = useDemoMode();
  return demo ? "/demo/dashboard" : "/dashboard";
}

export function dashboardPath(demo: boolean, subpath = ""): string {
  const base = demo ? "/demo/dashboard" : "/dashboard";
  if (!subpath) return base;
  return `${base}${subpath.startsWith("/") ? subpath : `/${subpath}`}`;
}

export function useDashboardHref(subpath = ""): string {
  const demo = useDemoMode();
  return useMemo(() => dashboardPath(demo, subpath), [demo, subpath]);
}

/** Map a /dashboard/... path to the current base (/dashboard or /demo/dashboard). */
export function resolveDashboardHref(basePath: string, href: string): string {
  if (href.startsWith("/demo/")) return href;
  if (!href.startsWith("/dashboard")) return href;
  const suffix = href === "/dashboard" ? "" : href.slice("/dashboard".length);
  return `${basePath}${suffix}`;
}
