import { Card } from "@/components/ui/card";

/**
 * Task collection loading fallback (issue #93): renders inside the project
 * layout's <main>, which keeps the sidebar and header on screen while the
 * task list itself loads — only the list area needs a skeleton.
 */
export default function TasksLoading() {
  return (
    <div>
      <div className="bg-muted h-7 w-24 animate-pulse rounded" />
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
