import { redirect } from "next/navigation";

// API keys management moved under Settings. Keep this redirect so old
// bookmarks and the bootstrap-time "manage keys" link don't 404.
export default function LegacyKeysPage() {
  redirect("/dashboard/settings/api-keys");
}
