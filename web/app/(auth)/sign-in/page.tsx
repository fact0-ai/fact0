import { Suspense } from "react";
import { headers } from "next/headers";
import { redirect } from "next/navigation";

import { AuthForm } from "@/components/auth/auth-form";
import { getAuth } from "@/lib/auth";
import {
  resolvePostAuthDestination,
  sanitiseRedirectPath,
} from "@/lib/app-origin";

type Props = {
  searchParams: Promise<{ redirect_url?: string }>;
};

export const dynamic = "force-dynamic";

export default async function SignInPage({ searchParams }: Props) {
  const requestHeaders = await headers();
  const session = await getAuth().api.getSession({ headers: requestHeaders });
  if (session?.user) {
    const params = await searchParams;
    redirect(
      resolvePostAuthDestination(sanitiseRedirectPath(params.redirect_url)),
    );
  }

  return (
    <Suspense fallback={<div className="h-96" />}>
      <AuthForm />
    </Suspense>
  );
}
