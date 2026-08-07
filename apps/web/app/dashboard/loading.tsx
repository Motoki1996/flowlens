import { Card } from "@/components/ui/card";

/**
 * Dashboard-specific loading fallback (issue #93): mirrors DashboardView's
 * 2-column grid of teaser cards so the layout doesn't jump once the real
 * content lands.
 */
export default function DashboardLoading() {
  return (
    <main className="mx-auto max-w-6xl px-6 py-8">
      <div className="bg-muted h-7 w-32 animate-pulse rounded" />
      <div className="bg-muted mt-2 h-4 w-48 animate-pulse rounded" />
      <div className="mt-8 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card className="h-40 animate-pulse" />
        <Card className="h-40 animate-pulse" />
        <Card className="h-40 animate-pulse" />
        <Card className="h-40 animate-pulse" />
      </div>
    </main>
  );
}
