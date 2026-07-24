import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default async function SettingsPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-foreground text-2xl font-semibold">Settings</h1>

        <Card className="mt-6">
          <CardHeader>
            <CardTitle className="text-base font-medium">Account</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">Username</dt>
                <dd className="text-foreground">{user.username}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Display name</dt>
                <dd className="text-foreground">{user.displayName || "—"}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Email</dt>
                <dd className="text-foreground">{user.email}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </main>
    </>
  );
}
