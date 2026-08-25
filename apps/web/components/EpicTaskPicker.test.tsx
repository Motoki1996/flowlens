import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Task } from "@/types";
import { EpicTaskPicker } from "./EpicTaskPicker";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    epicId: null,
    title: "Build the list screen",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: { acceptanceCriteria: "", aiContext: "", updatedAt: null },
    ...overrides,
  };
}

/** The rows are cmdk options. cmdk owns `aria-selected` for its keyboard
 *  cursor, so being in the epic is `aria-checked` — see the component. */
function row(name: string) {
  return screen.getByRole("option", { name: new RegExp(name) });
}

function queryRow(name: string) {
  return screen.queryByRole("option", { name: new RegExp(name) });
}

const tasks = [
  makeTask({ id: "t1", title: "Free task" }),
  makeTask({ id: "t2", title: "Already ours", epicId: "e1" }),
  makeTask({ id: "t3", title: "In another epic", epicId: "e2" }),
  makeTask({ id: "t4", title: "Another backlog", backlogId: "b2" }),
];

function renderPicker(props: Partial<Parameters<typeof EpicTaskPicker>[0]> = {}) {
  return render(
    <EpicTaskPicker
      id="picker"
      tasks={tasks}
      backlogId="b1"
      epicId="e1"
      value={["t2"]}
      onChange={vi.fn()}
      {...props}
    />,
  );
}

describe("EpicTaskPicker", () => {
  // The candidates are what makes this safe to use: it can only ever move a
  // task that is free to move.
  it("offers this backlog's free tasks and the epic's own, nothing else", () => {
    renderPicker();

    expect(row("Free task")).toHaveAttribute("aria-checked", "false");
    expect(row("Already ours")).toHaveAttribute("aria-checked", "true");
    // Taking a task out of another epic is a decision to make on that task.
    expect(queryRow("In another epic")).not.toBeInTheDocument();
    expect(queryRow("Another backlog")).not.toBeInTheDocument();
  });

  it("reports the whole set on each toggle", () => {
    const onChange = vi.fn();
    renderPicker({ onChange });

    fireEvent.click(row("Free task"));
    expect(onChange).toHaveBeenCalledWith(["t2", "t1"]);

    onChange.mockClear();
    fireEvent.click(row("Already ours"));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it("narrows the list by the search box", () => {
    renderPicker();

    fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "free" } });

    expect(row("Free task")).toBeInTheDocument();
    expect(queryRow("Already ours")).not.toBeInTheDocument();
  });

  it("says why the list is empty, per filter", () => {
    renderPicker({ value: [] });

    fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "zzz" } });
    expect(screen.getByText('No tasks match "zzz".')).toBeInTheDocument();

    // Nothing is selected, so "Selected only" empties the list for its own
    // reason and has to say which one it is.
    fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "" } });
    fireEvent.click(screen.getByLabelText("Selected only"));
    expect(screen.getByText("Nothing selected yet.")).toBeInTheDocument();
  });

  describe("closed tasks", () => {
    const withClosed = [
      makeTask({ id: "t1", title: "Open one" }),
      makeTask({ id: "t5", title: "Closed one", status: "closed" }),
    ];

    it("hides them by default and shows them on request", () => {
      renderPicker({ tasks: withClosed, value: [] });

      expect(queryRow("Closed one")).not.toBeInTheDocument();
      expect(screen.getByText("Select all (1)")).toBeInTheDocument();

      fireEvent.click(screen.getByLabelText("Open only"));
      expect(row("Closed one")).toBeInTheDocument();
      expect(screen.getByText("Select all (2)")).toBeInTheDocument();
    });

    // The set is saved whole, so anything already picked has to stay
    // reviewable — "Open only" narrows what to pick from, it doesn't hide
    // what was picked.
    it("keeps a closed task on screen while it is selected", () => {
      renderPicker({ tasks: withClosed, value: ["t5"] });
      expect(row("Closed one")).toHaveAttribute("aria-checked", "true");
    });

    it("explains an empty list caused by the filter", () => {
      renderPicker({ tasks: [makeTask({ id: "t5", title: "Closed one", status: "closed" })], value: [] });
      expect(screen.getByText(/Untick .Open only./)).toBeInTheDocument();
    });
  });

  // The one thing a filter must never do is let an action reach a row the
  // reader can't see. Both bulk controls state their own count for that
  // reason, and a selection hidden by the search survives either of them.
  describe("the bulk actions reach only what is visible", () => {
    const many = [
      makeTask({ id: "t1", title: "Alpha one" }),
      makeTask({ id: "t2", title: "Alpha two" }),
      makeTask({ id: "t3", title: "Beta" }),
    ];

    it("adds only the visible rows, keeping a hidden selection", () => {
      const onChange = vi.fn();
      renderPicker({ tasks: many, epicId: undefined, value: ["t3"], onChange });

      fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "alpha" } });
      expect(screen.getByText("Select all (2)")).toBeInTheDocument();
      fireEvent.click(screen.getByText("Select all (2)"));

      // "Beta" is selected but filtered out — it must survive.
      expect(onChange).toHaveBeenCalledWith(["t3", "t1", "t2"]);
    });

    it("clears only the visible rows, keeping a hidden selection", () => {
      const onChange = vi.fn();
      renderPicker({ tasks: many, epicId: undefined, value: ["t1", "t2", "t3"], onChange });

      fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "alpha" } });
      fireEvent.click(screen.getByText("Clear (2)"));

      expect(onChange).toHaveBeenCalledWith(["t3"]);
    });
  });

  it("narrows to the selection on request", () => {
    renderPicker();

    fireEvent.click(screen.getByLabelText("Selected only"));
    expect(row("Already ours")).toBeInTheDocument();
    expect(queryRow("Free task")).not.toBeInTheDocument();
  });

  it("counts the selection, including what a filter is hiding", () => {
    renderPicker({ value: ["t2"] });
    expect(screen.getByText("1 selected")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search tasks"), { target: { value: "zzz" } });
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  // An epic outside a backlog has nowhere to draw from: filing a task under
  // it would have no backlog to move the task to.
  it("says so rather than showing an empty list when the epic has no backlog", () => {
    renderPicker({ backlogId: null });
    expect(screen.getByText(/File it in a backlog first/)).toBeInTheDocument();
  });

  it("explains an empty candidate list", () => {
    renderPicker({ tasks: [makeTask({ id: "t3", title: "In another epic", epicId: "e2" })] });
    expect(screen.getByText(/No free tasks in this backlog/)).toBeInTheDocument();
  });
});
