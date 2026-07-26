import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { NewProjectForm } from "./NewProjectForm";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh }),
}));

describe("NewProjectForm", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows a validation error and does not call the API when the name is blank", async () => {
    const onCancel = vi.fn();
    render(<NewProjectForm onCancel={onCancel} />);

    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(await screen.findByText("Project name is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("submits the trimmed fields and refreshes on success", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ id: "1", name: "Alpha", description: "" }), { status: 201 }),
    );
    const onCancel = vi.fn();
    render(<NewProjectForm onCancel={onCancel} />);

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Alpha" } });
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    await waitFor(() => expect(onCancel).toHaveBeenCalled());
    expect(refresh).toHaveBeenCalled();
  });

  it("shows the API error message when creation fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "name_taken", message: "a project with this name already exists" } }),
        { status: 409 },
      ),
    );
    render(<NewProjectForm onCancel={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Alpha" } });
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
  });
});
