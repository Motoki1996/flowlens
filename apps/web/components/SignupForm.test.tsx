import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { SignupForm } from "./SignupForm";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

function fill(password: string, confirm: string) {
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: "octocat" } });
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "octocat@example.com" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: confirm } });
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));
}

describe("SignupForm", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("rejects a mismatched confirmation without calling the API", async () => {
    render(<SignupForm />);

    fill("hunter22", "hunter23");

    expect(await screen.findByText("Password and confirmation do not match.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("signs up when the confirmation matches", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }));
    render(<SignupForm />);

    fill("hunter22", "hunter22");

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/auth/signup",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ username: "octocat", email: "octocat@example.com", password: "hunter22" }),
        }),
      ),
    );
  });
});
