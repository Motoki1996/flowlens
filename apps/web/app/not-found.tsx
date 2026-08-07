import Link from "next/link";
import { getCurrentUser } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { BoundaryHeader } from "@/components/BoundaryHeader";
import { Button } from "@/components/ui/button";

/**
 * The app-wide 404 (issue #93). Every page that calls next/navigation's
 * notFound() — a task/backlog/project that doesn't exist or isn't owned by
 * the current user — falls back to this screen. Unlike error.tsx it's a
 * Server Component, so it can still fetch the session and show the real
 * AppHeader; BoundaryHeader is only the fallback for a signed-out visitor.
 */
export default async function NotFound() {
  let user = null;
  try {
    user = await getCurrentUser();
  } catch {
    // Falls back to BoundaryHeader below.
  }

  return (
    <>
      {user ? <AppHeader user={user} /> : <BoundaryHeader />}
      <main className="mx-auto flex max-w-6xl flex-col items-start gap-4 px-6 py-16">
        <h1 className="text-foreground text-2xl font-semibold">Page not found</h1>
        <p className="text-muted-foreground text-sm">
          It may have been moved, or you may not have access to it.
        </p>
        <Button asChild>
          <Link href={user ? "/dashboard" : "/login"}>{user ? "Go to dashboard" : "Go to login"}</Link>
        </Button>
      </main>
    </>
  );
}
