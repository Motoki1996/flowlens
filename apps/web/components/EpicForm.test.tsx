import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, Epic } from "@/types";
import { EpicForm } from "./EpicForm";

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  startDate: null,
  dueOn: null,
  priority: "medium",
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "main",
  allowedScope: "",
  forbiddenScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  taskCount: 0,
  closedTaskCount: 0,
  status: "open",
  closedAt: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function makeEpic(overrides: Partial<Epic> = {}): Epic {
  return {
    id: "e1",
    projectId: "p1",
    backlogId: "b1",
    name: "Screens",
    description: "",
    startDate: null,
    dueOn: null,
    priority: "medium",
    progress: "not_started",
    defaultLinkedGitlabProjectId: null,
    baseBranch: "",
    allowedScope: "",
    forbiddenScope: "",
    estimatedPoints: null,
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    taskCount: 0,
    closedTaskCount: 0,
    status: "open",
    closedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const onSaved = vi.fn();
const onCancel = vi.fn();

function bodyOf(fetchMock: ReturnType<typeof vi.fn>, call = 0) {
  const [, init] = fetchMock.mock.calls[call];
  return JSON.parse(init.body as string);
}

function okFetch(epic: Epic) {
  return vi.fn().mockResolvedValue({ ok: true, json: async () => epic });
}

describe("EpicForm", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  // estimatedPoints is the one epic field an agent writes and a human has to
  // be able to argue with, so the round trip through this form is the thing
  // worth pinning down — in particular that "" and 0 stay different, which is
  // the entire reason the API rejects 0.
  describe("the pre-breakdown estimate", () => {
    it("sends null when left empty, not zero", async () => {
      const fetchMock = okFetch(makeEpic());
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicForm
          projectId="p1"
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Screens" } });
      fireEvent.click(screen.getByRole("button", { name: "Create epic" }));

      await waitFor(() => expect(fetchMock).toHaveBeenCalled());
      expect(bodyOf(fetchMock).estimatedPoints).toBeNull();
    });

    it("sends the number as a number", async () => {
      const fetchMock = okFetch(makeEpic({ estimatedPoints: 11 }));
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicForm
          projectId="p1"
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Screens" } });
      fireEvent.change(screen.getByLabelText("Estimated points"), {
        target: { value: "11" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Create epic" }));

      await waitFor(() => expect(fetchMock).toHaveBeenCalled());
      expect(bodyOf(fetchMock).estimatedPoints).toBe(11);
    });

    // Caught here rather than left to the API's 400 so the message names the
    // field, and so the "leave it empty instead" advice is actually given.
    it("refuses 0 before sending, and says what to do instead", async () => {
      const fetchMock = okFetch(makeEpic());
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicForm
          projectId="p1"
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Screens" } });
      fireEvent.change(screen.getByLabelText("Estimated points"), {
        target: { value: "0" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Create epic" }));

      await waitFor(() =>
        expect(screen.getByText(/leave it empty for unestimated/)).toBeInTheDocument(),
      );
      expect(fetchMock).not.toHaveBeenCalled();
    });

    it("loads an existing estimate for editing", () => {
      render(
        <EpicForm
          projectId="p1"
          epic={makeEpic({ estimatedPoints: 21 })}
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      expect(screen.getByLabelText("Estimated points")).toHaveValue(21);
    });

    it("explains that tasks have taken over once the epic has some", () => {
      render(
        <EpicForm
          projectId="p1"
          epic={makeEpic({ estimatedPoints: 21, taskCount: 4 })}
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      expect(screen.getByText(/counts their sizes instead/)).toBeInTheDocument();
    });

    it("explains the cost of leaving it empty while the epic has none", () => {
      render(
        <EpicForm
          projectId="p1"
          epic={makeEpic()}
          backlogs={[backlog]}
          links={[]}
          onSaved={onSaved}
          onCancel={onCancel}
        />,
      );

      expect(
        screen.getByText(/the forecast cannot see this epic at all/),
      ).toBeInTheDocument();
    });
  });
});
