import { redirect } from "next/navigation";

export default function ToolsRedirectPage() {
  redirect("/dashboard/observability?tab=tools");
}
