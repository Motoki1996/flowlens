import { cookies } from "next/headers";
import { redirect, notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getMergeRequests,
  getProject,
  getProjects,
  getTasks,
} from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { ProjectSidebar, type ProjectSidebarCounts } from "@/components/ProjectSidebar";
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import {
  SIDEBAR_STATE_COOKIE,
  SIDEBAR_WIDTH_COOKIE,
  sidebarOpenFromCookie,
  sidebarWidthFromCookie,
} from "@/lib/sidebar";

/**
 * Every screen under a project shares this frame: the app header, then a
 * persistent sidebar carrying the project's own sections. Holding the project
 * context in the layout is what makes Backlogs → Tasks a single click instead
 * of a detour through the project's single view; the pages below only render
 * the object they are about.
 *
 * Auth is checked here only, not repeated in the nested pages below — this
 * layout renders the AppHeader they don't (issue #94). The sidebar's open
 * state and width are read from cookies here rather than on the client, so
 * the first paint is already the shape the user left it in. The project lookup
 * does still happen here as well as in each page: the page needs it for its
 * own data anyway, and the reads are memoised per request (see lib/api), so
 * the duplication costs one function call, not one round trip.
 */
export default async function ProjectLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ projectId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  // Everything below is decoration on the navigation: a failure drops the
  // count from the sidebar but must never take down the screen inside it.
  let counts: ProjectSidebarCounts = {
    backlogs: null,
    openTasks: null,
    totalTasks: null,
    mergeRequests: null,
    gitlab: null,
  };
  try {
    const [backlogs, tasks] = await Promise.all([getBacklogs(projectId), getTasks(projectId)]);
    counts = {
      ...counts,
      backlogs: backlogs.length,
      openTasks: tasks.filter((t) => t.status === "open").length,
      totalTasks: tasks.length,
    };
  } catch {
    // Counts stay null.
  }

  try {
    // perPage: 1 because only the badge's number is wanted here — totalCount
    // is counted in SQL, so the sidebar no longer pulls every merge request
    // of the project on every page load just to read .length off it.
    counts.mergeRequests = (await getMergeRequests(projectId, { perPage: 1 })).totalCount;
  } catch {
    // Left null — the section reads as "no summary".
  }

  try {
    const [connection, links] = await Promise.all([
      getGitlabConnection(projectId),
      getLinkedGitlabProjects(projectId),
    ]);
    // A broken connection is worth surfacing in the nav itself; a healthy one
    // is just how many projects it links.
    if (connection) {
      counts.gitlab = connection.lastVerifyError ? "Error" : String(links.length);
    }
  } catch {
    // Left null — the section reads as "no summary", not as "not connected".
  }

  let projects: Awaited<ReturnType<typeof getProjects>> = [];
  try {
    projects = await getProjects();
  } catch {
    // The switcher falls back to listing only the current project.
  }

  const cookieStore = await cookies();
  const sidebarOpen = sidebarOpenFromCookie(cookieStore.get(SIDEBAR_STATE_COOKIE)?.value);
  const sidebarWidth = sidebarWidthFromCookie(cookieStore.get(SIDEBAR_WIDTH_COOKIE)?.value);

  return (
    <SidebarProvider
      defaultOpen={sidebarOpen}
      style={{ "--sidebar-width": `${sidebarWidth}px` } as React.CSSProperties}
    >
      <ProjectSidebar project={project} projects={projects} counts={counts} />
      {/* shadcn's own SidebarInset is a <main>, and the page's content is
          already one — this is that element with the landmark left to the
          content below, where it belongs. */}
      <div className="bg-background relative flex min-w-0 flex-1 flex-col">
        <AppHeader user={user} leading={<SidebarTrigger className="-ml-1" />} />
        <main className="mx-auto w-full max-w-6xl min-w-0 flex-1 px-8 py-8">{children}</main>
      </div>
    </SidebarProvider>
  );
}
