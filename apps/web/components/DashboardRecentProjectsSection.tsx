import Link from "next/link";
import { formatDate } from "@/lib/dates";
import { projectPath } from "@/lib/routes";
import type { Project } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { TruncatedName } from "@/components/TruncatedName";

const MAX_ROWS = 5;

/**
 * DashboardRecentProjectsSection shortcuts to the most recently updated
 * projects, teasing into the Project collection at /projects rather than
 * growing its own project browsing (docs/ui-design.md rule 5).
 */
export function DashboardRecentProjectsSection({ projects }: { projects: Project[] }) {
  const visible = projects.slice(0, MAX_ROWS);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Projects</CardTitle>
          <Link
            href="/projects"
            className="text-muted-foreground hover:text-foreground text-xs hover:underline"
          >
            View all
          </Link>
        </div>
      </CardHeader>
      <CardContent>
        <ul className="space-y-2">
          {visible.map((project) => (
            <li key={project.id}>
              <Link
                href={projectPath(project.id)}
                className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
              >
                <TruncatedName text={project.name} className="text-foreground" />
                <span className="text-muted-foreground shrink-0 text-xs">
                  Updated {formatDate(project.updatedAt)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
