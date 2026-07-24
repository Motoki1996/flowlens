import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";

export default async function DashboardPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Dashboard</h1>
        <p className="mt-1 text-sm text-slate-600">
          Signed in as {user.username}.
        </p>

        {/* Empty state: project selection and metrics arrive in the next
            phase. This confirms authentication end-to-end today. */}
        <div className="mt-8 rounded-lg border border-dashed border-slate-300 bg-white p-10 text-center">
          <h2 className="text-base font-medium text-slate-800">
            No GitLab projects connected yet
          </h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-slate-600">
            GitLab CE project selection and merge-request metrics are coming in
            the next phase.
          </p>
        </div>
      </main>
    </>
  );
}
