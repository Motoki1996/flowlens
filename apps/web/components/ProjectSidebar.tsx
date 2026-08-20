"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  FolderIcon,
  GitPullRequestIcon,
  LayoutDashboardIcon,
  LayersIcon,
  ListTodoIcon,
  PlugIcon,
  type LucideIcon,
} from "lucide-react";
import type { Project } from "@/types";
import {
  projectSectionOf,
  projectSectionPath,
  type ProjectSection,
} from "@/lib/routes";
import { Combobox } from "@/components/ui/combobox";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { SidebarResizer } from "@/components/SidebarResizer";
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

/** The icon is what the section is known by once the sidebar is collapsed to
 *  its rail, so each one has to distinguish its section on its own. */
const SECTIONS: { section: ProjectSection; label: string; icon: LucideIcon }[] = [
  { section: "overview", label: "Overview", icon: LayoutDashboardIcon },
  { section: "backlogs", label: "Backlogs", icon: LayersIcon },
  { section: "tasks", label: "Tasks", icon: ListTodoIcon },
  { section: "merge-requests", label: "Merge requests", icon: GitPullRequestIcon },
  { section: "gitlab-connection", label: "GitLab connection", icon: PlugIcon },
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
 *
 * Built on shadcn's Sidebar, so it collapses to an icon rail (⌘B, or the
 * trigger in the header) and becomes a drawer on mobile; SidebarResizer adds
 * the width drag shadcn leaves out. Both pieces of that state live in cookies
 * the layout above reads, so a reload comes back the way it was left. It must
 * be rendered inside a SidebarProvider.
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
    <Sidebar collapsible="icon">
      {/* The switcher needs a text field to be worth anything, so the icon
          rail drops it rather than shrinking it into an unusable stub. */}
      <SidebarHeader className="group-data-[collapsible=icon]:hidden">
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
      </SidebarHeader>

      <SidebarContent>
        <nav aria-label="Project sections" className="p-2">
          <SidebarMenu>
            {SECTIONS.map(({ section, label, icon: Icon }) => {
              const active = section === current;
              const summary = summaryOf(section, counts);
              return (
                <SidebarMenuItem key={section}>
                  <SidebarMenuButton asChild isActive={active} tooltip={label}>
                    <Link
                      href={projectSectionPath(project.id, section)}
                      aria-current={active ? "page" : undefined}
                    >
                      <Icon />
                      <span className="flex-1 truncate">{label}</span>
                      {summary ? (
                        <span
                          className={cn(
                            "text-muted-foreground text-xs tabular-nums",
                            "group-data-[collapsible=icon]:hidden",
                          )}
                        >
                          {summary}
                        </span>
                      ) : null}
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              );
            })}
          </SidebarMenu>
        </nav>
      </SidebarContent>

      <SidebarFooter className="border-sidebar-border border-t">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild tooltip="All projects">
              <Link href="/projects" className="text-muted-foreground">
                <FolderIcon />
                <span>All projects</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>

      <SidebarResizer />
    </Sidebar>
  );
}
