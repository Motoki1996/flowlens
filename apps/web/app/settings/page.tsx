import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";

export default async function SettingsPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Settings</h1>

        <section className="mt-6 rounded-lg border border-slate-200 bg-white p-6">
          <h2 className="text-base font-medium text-slate-800">Account</h2>
          <dl className="mt-4 grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-slate-500">GitHub login</dt>
              <dd className="text-slate-900">{user.githubLogin}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Display name</dt>
              <dd className="text-slate-900">{user.displayName || "—"}</dd>
            </div>
            <div>
              <dt className="text-slate-500">GitHub user ID</dt>
              <dd className="text-slate-900">{user.githubUserId}</dd>
            </div>
          </dl>
        </section>
      </main>
    </>
  );
}
