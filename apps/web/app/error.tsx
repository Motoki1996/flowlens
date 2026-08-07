"use client";

import { useEffect } from "react";
import Link from "next/link";
import { BoundaryHeader } from "@/components/BoundaryHeader";
import { Button } from "@/components/ui/button";

/**
 * The app-wide error boundary (issue #93): every lib/api.ts reader throws on
 * a non-2xx response, and without this Next.js falls back to its own generic
 * error screen with no way back into the app. It is a Client Component by
 * Next.js's convention for error.tsx, so it cannot fetch the session itself —
 * BoundaryHeader stands in for AppHeader here.
 */
export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <>
      <BoundaryHeader />
      <main className="mx-auto flex max-w-6xl flex-col items-start gap-4 px-6 py-16">
        <h1 className="text-foreground text-2xl font-semibold">Something went wrong</h1>
        <p className="text-muted-foreground text-sm">
          The page couldn&apos;t load. You can try again, or head back to the dashboard.
        </p>
        <div className="flex gap-3">
          <Button onClick={() => reset()}>Try again</Button>
          <Button variant="outline" asChild>
            <Link href="/dashboard">Go to dashboard</Link>
          </Button>
        </div>
      </main>
    </>
  );
}
