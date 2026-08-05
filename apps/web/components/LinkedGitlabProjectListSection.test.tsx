import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { LinkedGitlabProjectListSection } from "./LinkedGitlabProjectListSection";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

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

  it("offers no Show more when the first page is the only one", async () => {
    vi.mocked(fetch).mockResolvedValue(searchResponse([{ id: 1, path: "group/one" }], 0));

    render(<LinkedGitlabProjectListSection projectId="p1" links={[]} connected />);
    fireEvent.click(screen.getByRole("button", { name: "Link a project" }));

    expect(await screen.findByRole("button", { name: "group/one" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });
});
