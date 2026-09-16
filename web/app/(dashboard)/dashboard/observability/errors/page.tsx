import { redirect } from "next/navigation";

export default function ErrorsRedirectPage() {
  redirect("/dashboard/observability?tab=errors");
}
