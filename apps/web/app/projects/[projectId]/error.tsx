"use client";

import { useEffect } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { projectSectionPath, type ProjectSection } from "@/lib/routes";
import { BoundaryHeader } from "@/components/BoundaryHeader";
import { Button } from "@/components/ui/button";

const SECTIONS: { section: ProjectSection; label: string }[] = [
  { section: "overview", label: "Overview" },
  { section: "backlogs", label: "Backlogs" },
  { section: "tasks", label: "Tasks" },
  { section: "gitlab-connection", label: "GitLab connection" },
];

/**
 * Error boundary for everything under /projects/[projectId] (issue #93). It
 * keeps the project's sidebar on screen — same idea as ProjectLayout, which
 * this boundary replaces — so a failure on one section (e.g. the task list)
 * doesn't strand the user outside the project. It's a Client Component by
 * Next.js's convention for error.tsx, so unlike ProjectLayout it can't fetch
 * the project or its counts; the sidebar here is section links only, built
 * from the route's own projectId (useParams), not project data.
 */
export default function ProjectError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  const params = useParams<{ projectId: string }>();
  const projectId = params.projectId;

  return (
    <>
      <BoundaryHeader />
      <div className="mx-auto flex max-w-7xl">
        <aside aria-label="Project" className="border-border w-60 shrink-0 border-r px-4 py-8">
          <div className="sticky top-8">
            <nav aria-label="Project sections">
              <ul className="space-y-0.5">
                {SECTIONS.map(({ section, label }) => (
                  <li key={section}>
                    <Link
                      href={projectSectionPath(projectId, section)}
                      className="text-muted-foreground hover:bg-accent/50 hover:text-foreground flex items-center rounded-md px-3 py-2 text-sm transition-colors"
                    >
                      {label}
                    </Link>
                  </li>
                ))}
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
        <main className="flex min-w-0 flex-1 flex-col items-start gap-4 px-8 py-16">
          <h1 className="text-foreground text-2xl font-semibold">Something went wrong</h1>
          <p className="text-muted-foreground text-sm">
            This section couldn&apos;t load. You can try again, or pick another section from the
            sidebar.
          </p>
          <Button onClick={() => reset()}>Try again</Button>
        </main>
      </div>
    </>
  );
}
