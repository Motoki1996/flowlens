import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";

export default async function DashboardPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-foreground text-2xl font-semibold">Dashboard</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Signed in as {user.username}.
        </p>

        {/* Empty state: project selection and metrics arrive in the next
            phase. This confirms authentication end-to-end today. */}
        <Card className="mt-8 border-dashed">
          <CardHeader className="text-center">
            <CardTitle className="text-base font-medium">
              No GitLab projects connected yet
            </CardTitle>
            <CardDescription className="mx-auto max-w-md">
              GitLab CE project selection and merge-request metrics are coming
              in the next phase.
            </CardDescription>
          </CardHeader>
        </Card>
      </main>
    </>
  );
}
