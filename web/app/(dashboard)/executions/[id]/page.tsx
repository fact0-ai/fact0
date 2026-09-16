import { redirect } from "next/navigation";

interface PageProps {
  params: Promise<{ id: string }>;
}

// Legacy URL - the canonical execution detail page now lives at
// /dashboard/executions/[id]. We keep this server-side redirect so any
// bookmarks, demo links, or in-app navigation from older Audit rows
// land in the right place.
export default async function LegacyExecutionDetailRedirect({ params }: PageProps) {
  const { id } = await params;
  redirect(`/dashboard/executions/${encodeURIComponent(id)}`);
}
