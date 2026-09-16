import { redirect } from "next/navigation";
import { headers } from "next/headers";

import { DashboardChrome } from "@/components/dashboard/dashboard-chrome";
import { getAuth } from "@/lib/auth";
import { signInHref } from "@/lib/app-origin";

export const dynamic = "force-dynamic";

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Server-side guard kept in addition to proxy.ts so direct subroute
  // fetches (eg. RSC payload) cannot leak chrome to anonymous callers.
  const requestHeaders = await headers();
  const session = await getAuth().api.getSession({ headers: requestHeaders });
  if (!session?.user) {
    redirect(signInHref("/dashboard"));
  }

  return <DashboardChrome>{children}</DashboardChrome>;
}
