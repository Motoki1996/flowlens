"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import type { Project } from "@/types";
import {
  projectSectionOf,
  projectSectionPath,
  type ProjectSection,
} from "@/lib/routes";
import { Combobox } from "@/components/ui/combobox";
import { cn } from "@/lib/utils";

/** Counts shown next to each section. `null` means the count failed to load —
 *  the link still works, it just carries no summary. */
export type ProjectSidebarCounts = {
  backlogs: number | null;
  openTasks: number | null;
  totalTasks: number | null;
  mergeRequests: number | null;
  gitlab: string | null;
};

const SECTIONS: { section: ProjectSection; label: string }[] = [
  { section: "overview", label: "Overview" },
  { section: "backlogs", label: "Backlogs" },
  { section: "tasks", label: "Tasks" },
  { section: "merge-requests", label: "Merge requests" },
  { section: "gitlab-connection", label: "GitLab connection" },
];

function summaryOf(section: ProjectSection, counts: ProjectSidebarCounts) {
  switch (section) {
    case "backlogs":
      return counts.backlogs === null ? null : String(counts.backlogs);
    case "tasks":
      return counts.openTasks === null || counts.totalTasks === null
        ? null
        : `${counts.openTasks}/${counts.totalTasks}`;
    case "merge-requests":
      return counts.mergeRequests === null ? null : String(counts.mergeRequests);
    case "gitlab-connection":
      return counts.gitlab;
    case "overview":
      return null;
  }
}

/**
 * ProjectSidebar keeps the project's context on screen for every screen under
 * /projects/[projectId]: its sibling collections are one click apart instead of
 * a detour through the project's own view, and the switcher at the top moves to
 * the same section of another project. It is the navigation half of
 * docs/ui-design.md rule 3, made permanent rather than left to each screen's
 * body.
 */
export function ProjectSidebar({
  project,
  projects,
  counts,
}: {
  project: Project;
  /** Every project the user owns, for the switcher. */
  projects: Project[];
  counts: ProjectSidebarCounts;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const current = projectSectionOf(pathname ?? "");

  // The switcher never lists fewer than the project we're on, even if the
  // project list failed to load.
  const options = (projects.length > 0 ? projects : [project]).map((p) => ({
    value: p.id,
    label: p.name,
  }));

  return (
    <aside
      aria-label="Project"
      className="border-border w-60 shrink-0 border-r px-4 py-8"
    >
      <div className="sticky top-8">
        <Combobox
          options={options}
          value={project.id}
          onChange={(projectId) => router.push(projectSectionPath(projectId, current))}
          aria-label="Switch project"
          searchPlaceholder="Search projects…"
          emptyText="No project found."
          size="sm"
          className="w-full"
        />

        <nav aria-label="Project sections" className="mt-4">
          <ul className="space-y-0.5">
            {SECTIONS.map(({ section, label }) => {
              const active = section === current;
              const summary = summaryOf(section, counts);
              return (
                <li key={section}>
                  <Link
                    href={projectSectionPath(project.id, section)}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "flex items-center justify-between gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                      active
                        ? "bg-accent text-accent-foreground font-medium"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    )}
                  >
                    <span>{label}</span>
                    {summary ? (
                      <span className="text-muted-foreground text-xs tabular-nums">{summary}</span>
                    ) : null}
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="border-border mt-4 border-t pt-4">
          <Link
            href="/projects"
            className="text-muted-foreground hover:text-foreground block px-3 text-sm"
          >
            All projects
          </Link>
        </div>
      </div>
    </aside>
  );
}
