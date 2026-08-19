import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { PasswordChangeSection } from "./PasswordChangeSection";

function fillForm(current: string, next: string, confirm = next) {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: current } });
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: next } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: confirm } });
}

function submit() {
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
}

describe("PasswordChangeSection", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("sends the current and new password, and confirms success", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<PasswordChangeSection />);

    fillForm("hunter22", "correct-horse");
    submit();

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/me/password",
        expect.objectContaining({
          method: "PUT",
          credentials: "include",
          body: JSON.stringify({ currentPassword: "hunter22", newPassword: "correct-horse" }),
        }),
      ),
    );
    expect(await screen.findByText("Password changed.")).toBeInTheDocument();
    // The fields are cleared so the new password is not left on screen.
    expect(screen.getByLabelText("New password")).toHaveValue("");
  });

  it("tells the reader other sessions are signed out, and this one is not", () => {
    render(<PasswordChangeSection />);
    expect(
      screen.getByText(/signs out every other session\. This one stays signed in\./),
    ).toBeInTheDocument();
  });

  it("catches a mistyped confirmation without calling the API", () => {
    render(<PasswordChangeSection />);

    fillForm("hunter22", "correct-horse", "correct-hoarse");
    submit();

    expect(screen.getByText("New password and confirmation do not match.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("rejects a too-short new password before calling the API", () => {
    render(<PasswordChangeSection />);

    fillForm("hunter22", "short");
    submit();

    expect(screen.getByText("New password must be at least 8 characters.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("surfaces the API's message when the current password is wrong", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "invalid_credentials", message: "current password is incorrect" } }),
        { status: 401 },
      ),
    );
    render(<PasswordChangeSection />);

    fillForm("wrong-one", "correct-horse");
    submit();

    expect(await screen.findByText("current password is incorrect")).toBeInTheDocument();
    expect(screen.queryByText("Password changed.")).not.toBeInTheDocument();
  });
});
