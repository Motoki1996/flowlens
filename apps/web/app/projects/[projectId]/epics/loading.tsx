import { Card } from "@/components/ui/card";

/**
 * Epic collection loading fallback, mirroring the Backlog collection's own:
 * renders inside the project layout's <main>, so only the list area needs a
 * skeleton.
 */
export default function EpicsLoading() {
  return (
    <div>
      <div className="bg-muted h-7 w-28 animate-pulse rounded" />
      <div className="mt-6 space-y-2">
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
        <Card className="h-14 animate-pulse" />
      </div>
    </div>
  );
}
