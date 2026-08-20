import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { TaskAIContext } from "@/types";
import { AIContextSection } from "./AIContextSection";

const emptyContext: TaskAIContext = {
  acceptanceCriteria: "",
  aiContext: "",
  updatedAt: null,
};

describe("AIContextSection", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows placeholders when no AI context is set", () => {
    render(<AIContextSection taskId="t1" aiContext={emptyContext} />);
    expect(screen.getByText("No acceptance criteria set.")).toBeInTheDocument();
    expect(screen.getByText("No AI context set.")).toBeInTheDocument();
  });

  it("edits and saves one field independently, leaving the others untouched", async () => {
    const updated: TaskAIContext = {
      ...emptyContext,
      acceptanceCriteria: "Ship the feature",
      updatedAt: "2026-01-01T00:00:00Z",
    };
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(updated), { status: 200 }));

    render(<AIContextSection taskId="t1" aiContext={emptyContext} />);

    fireEvent.click(screen.getAllByRole("button", { name: "Edit" })[0]);
    fireEvent.change(screen.getByLabelText("Acceptance criteria"), {
      target: { value: "Ship the feature" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByText("Ship the feature")).toBeInTheDocument());

    const [url, init] = vi.mocked(fetch).mock.calls[0];
    expect(url).toBe("/api/v1/tasks/t1/ai-context");
    expect(JSON.parse(init?.body as string)).toEqual({
      acceptanceCriteria: "Ship the feature",
      aiContext: "",
    });
  });

  it("shows an error and keeps editing open when the save fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid", message: "Too long" } }), {
        status: 400,
      }),
    );

    render(<AIContextSection taskId="t1" aiContext={emptyContext} />);
    fireEvent.click(screen.getAllByRole("button", { name: "Edit" })[0]);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Too long")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });
});
