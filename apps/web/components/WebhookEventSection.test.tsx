import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { LinkedGitlabProject, WebhookEvent } from "@/types";
import { WebhookEventSection } from "./WebhookEventSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

const link: LinkedGitlabProject = {
  id: "link-1",
  gitlabConnectionId: "conn-1",
  gitlabProjectId: 42,
  pathWithNamespace: "group/demo",
  name: "demo",
  webUrl: "https://gitlab.example.com/group/demo",
  syncScope: "all",
  syncLabels: [],
  isDefault: true,
  initialImportStatus: "completed",
  lastSyncedAt: "2026-01-01T00:00:00Z",
  webhookStatus: "registered",
  webhookRegisteredAt: "2026-01-01T00:00:00Z",
  webhookError: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function makeEvent(overrides: Partial<WebhookEvent>): WebhookEvent {
  return {
    id: "event-1",
    linkedGitlabProjectId: "link-1",
    eventName: "Issue Hook",
    objectKind: "issue",
    gitlabIssueIid: 7,
    status: "processed",
    skipReason: "",
    errorMessage: "",
    receivedAt: "2026-01-01T00:00:00Z",
    processedAt: "2026-01-01T00:00:01Z",
    ...overrides,
  };
}

describe("WebhookEventSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("renders nothing when no GitLab project is linked", () => {
    const { container } = render(<WebhookEventSection linkedProjects={[]} webhookEventsByLink={{}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows a placeholder when a linked project has no events yet", () => {
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{}} />);
    expect(screen.getByText("group/demo")).toBeInTheDocument();
    expect(screen.getByText("No webhook events received yet.")).toBeInTheDocument();
  });

  it("shows a processed event", () => {
    const event = makeEvent({ status: "processed" });
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{ "link-1": [event] }} />);
    expect(screen.getByText("Processed")).toBeInTheDocument();
    expect(screen.getByText("Issue Hook #7")).toBeInTheDocument();
  });

  it("shows the skip reason for a skipped event", () => {
    const event = makeEvent({ status: "skipped", skipReason: "stale" });
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{ "link-1": [event] }} />);
    expect(screen.getByText("Skipped")).toBeInTheDocument();
    expect(screen.getByText("Skipped: Stale delivery")).toBeInTheDocument();
  });

  it("warns and shows a retry button when an event failed", () => {
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{ "link-1": [event] }} />);
    expect(screen.getByText(/1 webhook event failed to apply/)).toBeInTheDocument();
    expect(screen.getByText("gitlab unreachable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("retries a failed event", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{ "link-1": [event] }} />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/linked-gitlab-projects/link-1/webhook-events/event-1/retry"),
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows an inline error when retry fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "event_not_failed", message: "not failed" } }), {
        status: 409,
      }),
    );
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkedProjects={[link]} webhookEventsByLink={{ "link-1": [event] }} />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByText("not failed")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
