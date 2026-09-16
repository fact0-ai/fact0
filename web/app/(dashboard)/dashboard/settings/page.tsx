import { docsHref } from "@/lib/docs-origin";
export default function SettingsPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Local installation</h1>
      <p className="text-sm text-muted-foreground">
        Fact0 experimental edition runs one owner and one workspace. Data is
        stored in your PostgreSQL database. There are no subscriptions or hosted
        account services.
      </p>
      <p className="text-sm">
        Manage API keys here. Change the owner password with the local reset
        command. Configure retention, backups and the public origin in your
        deployment.
      </p>
      <a className="text-sm underline" href={docsHref()}>
        Installation and operating guide
      </a>
    </div>
  );
}
