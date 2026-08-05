import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { WebhookEvent } from "@/types";
import { WebhookEventSection } from "./WebhookEventSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

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

  it("shows a placeholder when the link has no events yet", () => {
    render(<WebhookEventSection linkId="link-1" events={[]} />);
    expect(screen.getByText("Webhook events")).toBeInTheDocument();
    expect(screen.getByText("No webhook events received yet.")).toBeInTheDocument();
  });

  it("shows a processed event", () => {
    const event = makeEvent({ status: "processed" });
    render(<WebhookEventSection linkId="link-1" events={[event]} />);
    expect(screen.getByText("Processed")).toBeInTheDocument();
    expect(screen.getByText("Issue Hook #7")).toBeInTheDocument();
  });

  it("shows the skip reason for a skipped event", () => {
    const event = makeEvent({ status: "skipped", skipReason: "stale" });
    render(<WebhookEventSection linkId="link-1" events={[event]} />);
    expect(screen.getByText("Skipped")).toBeInTheDocument();
    expect(screen.getByText("Skipped: Stale delivery")).toBeInTheDocument();
  });

  it("warns and shows a retry button when an event failed", () => {
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkId="link-1" events={[event]} />);
    expect(screen.getByText(/1 webhook event failed to apply/)).toBeInTheDocument();
    expect(screen.getByText("gitlab unreachable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("retries a failed event", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkId="link-1" events={[event]} />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/linked-gitlab-projects/link-1/webhook-events/event-1/retry"),
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("offers Show more only while the API reports a next page", () => {
    const { rerender } = render(
      <WebhookEventSection linkId="link-1" events={[makeEvent({})]} nextPage={0} />,
    );
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();

    rerender(<WebhookEventSection linkId="link-1" events={[makeEvent({})]} nextPage={2} />);
    expect(screen.getByRole("button", { name: "Show more" })).toBeInTheDocument();
  });

  it("appends the next page of events and stops offering more at the end", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          events: [makeEvent({ id: "event-2", gitlabIssueIid: 8 })],
          nextPage: 0,
        }),
        { status: 200 },
      ),
    );
    render(<WebhookEventSection linkId="link-1" events={[makeEvent({})]} nextPage={2} />);

    fireEvent.click(screen.getByRole("button", { name: "Show more" }));

    expect(await screen.findByText("Issue Hook #8")).toBeInTheDocument();
    // The first page is still there — a page is appended, not swapped in.
    expect(screen.getByText("Issue Hook #7")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/linked-gitlab-projects/link-1/webhook-events?page=2"),
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("fetches a delivery's payload only when it is opened", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...makeEvent({}), payload: { object_kind: "issue" } }), {
        status: 200,
      }),
    );
    render(<WebhookEventSection linkId="link-1" events={[makeEvent({})]} />);
    expect(fetch).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "View payload" }));

    expect(await screen.findByText(/"object_kind": "issue"/)).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/linked-gitlab-projects/link-1/webhook-events/event-1"),
      expect.objectContaining({ credentials: "include" }),
    );

    // Closing and reopening reuses what was already fetched.
    fireEvent.click(screen.getByRole("button", { name: "Hide payload" }));
    fireEvent.click(screen.getByRole("button", { name: "View payload" }));
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("shows an inline error when retry fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "event_not_failed", message: "not failed" } }), {
        status: 409,
      }),
    );
    const event = makeEvent({ status: "failed", errorMessage: "gitlab unreachable" });
    render(<WebhookEventSection linkId="link-1" events={[event]} />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByText("not failed")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
