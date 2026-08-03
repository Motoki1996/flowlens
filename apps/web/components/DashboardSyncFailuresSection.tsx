import Link from "next/link";
import { projectPath } from "@/lib/routes";
import type { Project } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

/**
 * DashboardSyncFailuresSection lists every project with at least one
 * failed-sync task (issue #77), teasing into that project's own single view
 * — the sync warning banner and the fix (opening the task and retrying)
 * live there (ProjectDetail), not here (docs/ui-design.md rule 4).
 */
export function DashboardSyncFailuresSection({ projects }: { projects: Project[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">
          Sync failures
          {projects.length > 0 ? (
            <span className="text-muted-foreground ml-1 font-normal">({projects.length})</span>
          ) : null}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {projects.length === 0 ? (
          <p className="text-muted-foreground text-sm">No sync failures.</p>
        ) : (
          <ul className="space-y-2">
            {projects.map((project) => (
              <li key={project.id}>
                <Link
                  href={projectPath(project.id)}
                  className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                >
                  <span className="text-foreground truncate">{project.name}</span>
                  <Badge
                    variant="outline"
                    className="border-destructive/50 text-destructive shrink-0"
                  >
                    {project.failedSyncTaskCount} failed
                  </Badge>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
