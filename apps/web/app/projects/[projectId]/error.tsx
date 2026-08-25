"use client";

import { useEffect } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  FolderIcon,
  LayersIcon,
  LayoutDashboardIcon,
  ListTodoIcon,
  PlugIcon,
  type LucideIcon,
} from "lucide-react";
import { projectSectionPath, type ProjectSection } from "@/lib/routes";
import { BoundaryHeader } from "@/components/BoundaryHeader";
import { SidebarToggle } from "@/components/SidebarToggle";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";

const SECTIONS: { section: ProjectSection; label: string; icon: LucideIcon }[] = [
  { section: "overview", label: "Overview", icon: LayoutDashboardIcon },
  { section: "backlogs", label: "Backlogs", icon: LayersIcon },
  { section: "tasks", label: "Tasks", icon: ListTodoIcon },
  { section: "gitlab-connection", label: "GitLab connection", icon: PlugIcon },
];

/**
 * Error boundary for everything under /projects/[projectId] (issue #93). It
 * keeps the project's sidebar on screen — same idea as ProjectLayout, which
 * this boundary replaces — so a failure on one section (e.g. the task list)
 * doesn't strand the user outside the project. It's a Client Component by
 * Next.js's convention for error.tsx, so unlike ProjectLayout it can't fetch
 * the project or its counts; the sidebar here is section links only, built
 * from the route's own projectId (useParams), not project data. It collapses
 * the same way, but starts open every time: the cookie the layout reads is
 * server-side state this boundary has no access to.
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
    <SidebarProvider>
      <Sidebar collapsible="icon">
        {/* No switcher to sit beside it here — this boundary has no project
            data — but the toggle still belongs to the sidebar it collapses. */}
        <SidebarHeader className="flex-row items-center justify-end">
          <SidebarToggle placement="sidebar" />
        </SidebarHeader>
        <SidebarContent>
          <nav aria-label="Project sections" className="p-2">
            <SidebarMenu>
              {SECTIONS.map(({ section, label, icon: Icon }) => (
                <SidebarMenuItem key={section}>
                  <SidebarMenuButton asChild tooltip={label}>
                    <Link href={projectSectionPath(projectId, section)}>
                      <Icon />
                      <span>{label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
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
      </Sidebar>
      <div className="bg-background relative flex min-w-0 flex-1 flex-col">
        <BoundaryHeader leading={<SidebarToggle />} />
        <main className="flex min-w-0 flex-1 flex-col items-start gap-4 px-8 py-16">
          <h1 className="text-foreground text-2xl font-semibold">Something went wrong</h1>
          <p className="text-muted-foreground text-sm">
            This section couldn&apos;t load. You can try again, or pick another section from the
            sidebar.
          </p>
          <Button onClick={() => reset()}>Try again</Button>
        </main>
      </div>
    </SidebarProvider>
  );
}
