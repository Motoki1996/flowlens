import { Card } from "@/components/ui/card";

/**
 * Backlog collection loading fallback, mirroring the Task collection's own
 * (issue #93): renders inside the project layout's <main>, so only the list
 * area needs a skeleton.
 */
export default function BacklogsLoading() {
  return (
    <div>
      <div className="bg-muted h-7 w-28 animate-pulse rounded" />
      <div className="mt-6 space-y-2">
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
      </div>
    </div>
  );
}
