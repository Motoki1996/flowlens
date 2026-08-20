import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { GitlabIdentitySection } from "./GitlabIdentitySection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

describe("GitlabIdentitySection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });

  it("shows an empty state when no identity is registered", () => {
    render(<GitlabIdentitySection identities={[]} />);
    expect(screen.getByText("No GitLab identity registered yet.")).toBeInTheDocument();
  });

  it("lists a registered identity's base URL, username and user ID", () => {
    render(
      <GitlabIdentitySection
        identities={[
          { id: "id-1", gitlabBaseUrl: "https://gitlab.example.com", gitlabUserId: 42, gitlabUsername: "alice" },
        ]}
      />,
    );
    expect(screen.getByText("https://gitlab.example.com")).toBeInTheDocument();
    expect(screen.getByText(/alice · user ID 42/)).toBeInTheDocument();
  });

  it("strips a trailing slash from the base URL before submitting", async () => {
    render(<GitlabIdentitySection identities={[]} />);

    fireEvent.change(screen.getByLabelText("GitLab base URL"), {
      target: { value: "https://gitlab.example.com/" },
    });
    fireEvent.change(screen.getByLabelText("GitLab user ID"), { target: { value: "42" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(fetch).toHaveBeenCalled());
    const [, options] = vi.mocked(fetch).mock.calls[0];
    const body = JSON.parse(options?.body as string);
    expect(body.gitlabBaseUrl).toBe("https://gitlab.example.com");
  });
});
