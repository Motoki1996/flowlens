import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import type { GitlabConnection, Project } from "@/types";
import { ProjectDetail } from "./ProjectDetail";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
  usePathname: () => "/projects/1",
  useSearchParams: () => new URLSearchParams(),
}));

const project: Project = {
  id: "1",
  name: "Alpha",
  description: "The first project",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-02T00:00:00Z",
  failedSyncTaskCount: 0,
};

const connection: GitlabConnection = {
  projectId: "1",
  baseUrl: "https://gitlab.example.com",
  tokenLastFour: "a1b2",
  tokenGitlabUserId: 42,
  tokenGitlabUsername: "octocat",
  verified: true,
  lastVerifiedAt: "2026-01-05T09:00:00Z",
  lastVerifyError: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-05T09:00:00Z",
};

describe("ProjectDetail", () => {
  beforeEach(() => {
    push.mockClear();
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows identity and attributes in order, with edit and delete actions", () => {
    render(<ProjectDetail currentUserId="me" project={project} />);
    expect(screen.getByRole("heading", { name: "Alpha" })).toBeInTheDocument();
    expect(screen.getByText("The first project")).toBeInTheDocument();
    expect(screen.getByText("Created")).toBeInTheDocument();
    expect(screen.getByText("Updated")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });

  it("links to its backlog and task collections with their counts", () => {
    render(
      <ProjectDetail
        currentUserId="me"
        project={project}
        backlogCount={2}
        taskCount={5}
        openTaskCount={3}
      />,
    );
    expect(screen.getByRole("link", { name: /Backlogs/ })).toHaveAttribute(
      "href",
      "/projects/1/backlogs",
    );
    expect(screen.getByText("2 backlogs")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Tasks/ })).toHaveAttribute("href", "/projects/1/tasks");
    expect(screen.getByText("3 open / 5 total")).toBeInTheDocument();
  });

  it("summarises the GitLab connection's state on its link", () => {
    const { rerender } = render(<ProjectDetail currentUserId="me" project={project} />);
    expect(screen.getByRole("link", { name: /GitLab connection/ })).toHaveAttribute(
      "href",
      "/projects/1/gitlab-connection",
    );
    expect(screen.getByText("Not connected")).toBeInTheDocument();

    rerender(
      <ProjectDetail
        currentUserId="me"
        project={project}
        gitlabConnection={connection}
        linkedProjectCount={2}
      />,
    );
    expect(screen.getByText("2 linked projects")).toBeInTheDocument();

    rerender(
      <ProjectDetail
        currentUserId="me"
        project={project}
        gitlabConnection={{ ...connection, verified: false, lastVerifyError: "token rejected" }}
      />,
    );
    expect(screen.getByText("Connection invalid")).toBeInTheDocument();
  });

  it("still links to the collections when their counts fail to load", () => {
    render(<ProjectDetail currentUserId="me" project={project} countsError />);
    expect(screen.getByRole("link", { name: /Tasks/ })).toHaveAttribute("href", "/projects/1/tasks");
    expect(screen.getAllByText("Count unavailable")).toHaveLength(2);
  });

  it("shows no sync warning when no tasks have failed to sync", () => {
    render(<ProjectDetail currentUserId="me" project={project} />);
    expect(screen.queryByText(/failed to sync with GitLab/)).not.toBeInTheDocument();
  });

  it("warns when tasks have failed to sync with GitLab", () => {
    render(
      <ProjectDetail currentUserId="me" project={{ ...project, failedSyncTaskCount: 2 }} />,
    );
    expect(screen.getByText("2 tasks failed to sync with GitLab. Open a task from Tasks to see the error and retry.")).toBeInTheDocument();
  });

  it("requires a confirmation step before deleting", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<ProjectDetail currentUserId="me" project={project} />);

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getByText("Delete this project?")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/projects"));
  });

  it("switches to the edit form and shows saved changes", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...project, name: "Renamed" }), { status: 200 }),
    );
    render(<ProjectDetail currentUserId="me" project={project} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const editForm = screen.getByRole("form", { name: "Edit project" });
    fireEvent.change(within(editForm).getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(within(editForm).getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("heading", { name: "Renamed" })).toBeInTheDocument();
  });
});
