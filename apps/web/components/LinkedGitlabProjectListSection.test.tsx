import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { LinkedGitlabProjectListSection } from "./LinkedGitlabProjectListSection";
import type { LinkedGitlabProject } from "@/types";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

function makeLink(overrides: Partial<LinkedGitlabProject>): LinkedGitlabProject {
  return {
    id: "l1",
    gitlabConnectionId: "c1",
    gitlabProjectId: 100,
    pathWithNamespace: "team/flowlens-demo",
    name: "flowlens-demo",
    webUrl: "https://gitlab.example.com/team/flowlens-demo",
    syncScope: "all",
    syncLabels: [],
    isDefault: false,
    initialImportStatus: "completed",
    lastSyncedAt: "2026-01-06T12:00:00Z",
    webhookStatus: "registered",
    webhookRegisteredAt: "2026-01-05T09:05:00Z",
    webhookError: "",
    createdAt: "2026-01-05T09:00:00Z",
    updatedAt: "2026-01-06T12:00:00Z",
    ...overrides,
  };
}

function searchResponse(projects: { id: number; path: string }[], nextPage: number) {
  return new Response(
    JSON.stringify({
      projects: projects.map((p) => ({
        id: p.id,
        name: p.path.split("/").pop(),
        pathWithNamespace: p.path,
        webUrl: `https://gitlab.example.com/${p.path}`,
      })),
      nextPage,
    }),
    { status: 200 },
  );
}

describe("LinkedGitlabProjectListSection", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  // The GitLab project search is paged by the API; a token that can see many
  // projects must not be limited to whatever fits on the first page.
  it("pages through the GitLab project search", async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(searchResponse([{ id: 1, path: "group/one" }], 2))
      .mockResolvedValueOnce(searchResponse([{ id: 2, path: "group/two" }], 0));

    render(<LinkedGitlabProjectListSection projectId="p1" links={[]} connected />);
    fireEvent.click(screen.getByRole("button", { name: "Link a project" }));

    expect(await screen.findByRole("button", { name: "group/one" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show more" }));

    expect(await screen.findByRole("button", { name: "group/two" })).toBeInTheDocument();
    // The first page stays on screen, and the end of the list retires the action.
    expect(screen.getByRole("button", { name: "group/one" })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument(),
    );
    expect(vi.mocked(fetch).mock.calls[1][0]).toContain("page=2");
  });

  // Which link a task with no link of its own is pushed to is otherwise only
  // visible on each link's own view, one click away from the list.
  it("badges the default link, and only that one", () => {
    render(
      <LinkedGitlabProjectListSection
        projectId="p1"
        links={[
          makeLink({ id: "l1", pathWithNamespace: "team/api", isDefault: false }),
          makeLink({ id: "l2", pathWithNamespace: "team/web", isDefault: true }),
        ]}
        connected
      />,
    );

    const rows = screen.getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("team/api");
    expect(rows[0]).not.toHaveTextContent("Default");
    expect(rows[1]).toHaveTextContent("team/web");
    expect(rows[1]).toHaveTextContent("Default");
  });

  it("offers no Show more when the first page is the only one", async () => {
    vi.mocked(fetch).mockResolvedValue(searchResponse([{ id: 1, path: "group/one" }], 0));

    render(<LinkedGitlabProjectListSection projectId="p1" links={[]} connected />);
    fireEvent.click(screen.getByRole("button", { name: "Link a project" }));

    expect(await screen.findByRole("button", { name: "group/one" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });
});
