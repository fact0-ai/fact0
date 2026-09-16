"use client";

import { usePathname } from "next/navigation";

import { AdminGuard } from "@/components/dashboard/admin-guard";
import { SettingsNav } from "@/components/dashboard/settings-nav";

export function SettingsLayoutBody({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const isAccountSettings = pathname.includes("/settings/account");

  const body = (
    <div className="grid grid-cols-1 lg:grid-cols-[220px_minmax(0,1fr)] gap-8 lg:gap-12 max-w-6xl">
      <SettingsNav />
      <section className="min-w-0">{children}</section>
    </div>
  );

  if (isAccountSettings) return body;
  return <AdminGuard>{body}</AdminGuard>;
}
