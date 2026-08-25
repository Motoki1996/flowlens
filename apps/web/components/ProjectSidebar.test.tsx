import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Project } from "@/types";
import { SidebarProvider } from "@/components/ui/sidebar";
import { ProjectSidebar, type ProjectSidebarCounts } from "./ProjectSidebar";

const push = vi.fn();
let pathname = "/projects/p1";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh: vi.fn() }),
  usePathname: () => pathname,
}));

function project(id: string, name: string): Project {
  return {
    id,
    name,
    description: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    failedSyncTaskCount: 0,
  };
}

const alpha = project("p1", "Alpha");
const beta = project("p2", "Beta");

const counts: ProjectSidebarCounts = {
  backlogs: 2,
  epics: 2,
  openTasks: 3,
  totalTasks: 7,
  mergeRequests: 5,
  gitlab: "1",
};

/** ProjectSidebar reads its open/collapsed state from the provider the layout
 *  owns, so every case renders inside one. */
function renderSidebar(
  overrides: Partial<Parameters<typeof ProjectSidebar>[0]> = {},
  providerProps: Partial<Parameters<typeof SidebarProvider>[0]> = {},
) {
  return render(
    <SidebarProvider {...providerProps}>
      <ProjectSidebar project={alpha} projects={[alpha, beta]} counts={counts} {...overrides} />
    </SidebarProvider>,
  );
}

describe("ProjectSidebar", () => {
  beforeEach(() => {
    push.mockClear();
    pathname = "/projects/p1";
  });

  it("links every section of the project so siblings are one click apart", () => {
    renderSidebar();
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    const links: [string, string][] = [
      ["Overview", "/projects/p1"],
      ["Backlogs", "/projects/p1/backlogs"],
      ["Tasks", "/projects/p1/tasks"],
      ["Merge requests", "/projects/p1/merge-requests"],
      ["GitLab connection", "/projects/p1/gitlab-connection"],
    ];
    for (const [name, href] of links) {
      expect(nav.getByRole("link", { name: new RegExp(`^${name}`) })).toHaveAttribute("href", href);
    }
  });

  it("summarises each collection next to its link", () => {
    renderSidebar();
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: /^Backlogs/ })).toHaveTextContent("2");
    expect(nav.getByRole("link", { name: /^Tasks/ })).toHaveTextContent("3/7");
    expect(nav.getByRole("link", { name: /^Merge requests/ })).toHaveTextContent("5");
    expect(nav.getByRole("link", { name: /^GitLab connection/ })).toHaveTextContent("1");
  });

  it("drops the summary rather than the link when a count is unavailable", () => {
    renderSidebar({
      counts: { backlogs: null, epics: null, openTasks: null, totalTasks: null, mergeRequests: null, gitlab: null },
    });
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: "Backlogs" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs",
    );
  });

  // Which entry is highlighted is read from the URL, so a single view marks the
  // collection it belongs to rather than nothing at all.
  const activeCases: [string, string][] = [
    ["/projects/p1", "Overview"],
    ["/projects/p1/backlogs", "Backlogs"],
    ["/projects/p1/backlogs/b1", "Backlogs"],
    ["/projects/p1/tasks", "Tasks"],
    ["/projects/p1/tasks/t1", "Tasks"],
    ["/projects/p1/gitlab-connection", "GitLab connection"],
    ["/projects/p1/linked-gitlab-projects/l1", "GitLab connection"],
  ];
  for (const [path, expected] of activeCases) {
    it(`marks ${expected} as the current section on ${path}`, () => {
      pathname = path;
      renderSidebar();
      const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
      const current = nav.getAllByRole("link").filter((l) => l.getAttribute("aria-current"));
      expect(current).toHaveLength(1);
      expect(current[0]).toHaveTextContent(expected);
    });
  }

  it("switches project while staying on the same section", async () => {
    pathname = "/projects/p1/tasks";
    renderSidebar();
    await userEvent.click(screen.getByRole("combobox", { name: "Switch project" }));
    await userEvent.click(screen.getByRole("option", { name: "Beta" }));
    expect(push).toHaveBeenCalledWith("/projects/p2/tasks");
  });

  it("falls back to the current project when the project list failed to load", () => {
    renderSidebar({ projects: [] });
    expect(screen.getByRole("combobox", { name: "Switch project" })).toHaveTextContent("Alpha");
  });
});
