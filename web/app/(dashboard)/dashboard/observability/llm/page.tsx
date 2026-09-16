import { redirect } from "next/navigation";

export default function LLMRedirectPage() {
  redirect("/dashboard/observability?tab=llm");
}
