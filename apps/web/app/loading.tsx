import { Card } from "@/components/ui/card";

/**
 * Route-wide loading fallback (issue #93): shown while a page's server
 * component is still fetching, for every screen that doesn't have a more
 * specific loading.tsx of its own (see app/dashboard and
 * app/projects/[projectId]/tasks for those).
 */
export default function Loading() {
  return (
    <main className="mx-auto max-w-6xl px-6 py-8">
      <div className="bg-muted h-7 w-48 animate-pulse rounded" />
      <div className="mt-6 space-y-3">
        <Card className="h-20 animate-pulse" />
        <Card className="h-20 animate-pulse" />
        <Card className="h-20 animate-pulse" />
      </div>
    </main>
  );
}
