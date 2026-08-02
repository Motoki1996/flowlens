import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import type { ApiToken, ApiTokenWithSecret } from "@/types";
import { ApiTokenSection } from "./ApiTokenSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

function makeToken(overrides: Partial<ApiToken> = {}): ApiToken {
  return {
    id: "token-1",
    projectId: "p1",
    name: "CI agent",
    scopes: ["read"],
    tokenPrefix: "flt_a1b2c3d4",
    lastUsedAt: null,
    expiresAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ApiTokenSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows an empty state explaining the feature when no tokens exist", () => {
    render(<ApiTokenSection projectId="p1" tokens={[]} />);
    expect(screen.getByText("API tokens")).toBeInTheDocument();
    expect(screen.getByText(/No tokens issued yet\.$/)).toBeInTheDocument();
  });

  it("lists a token's name, prefix, scope, last used and expiry", () => {
    const token = makeToken({ lastUsedAt: "2026-02-01T00:00:00Z", expiresAt: "2027-01-01T00:00:00Z" });
    render(<ApiTokenSection projectId="p1" tokens={[token]} />);
    expect(screen.getByText("CI agent")).toBeInTheDocument();
    expect(screen.getByText("flt_a1b2c3d4…")).toBeInTheDocument();
    expect(screen.getByText("Read-only")).toBeInTheDocument();
    expect(screen.queryByText("Never")).not.toBeInTheDocument();
  });

  it("shows read & write scope and 'Never' for unused/non-expiring tokens", () => {
    const token = makeToken({ scopes: ["read", "write"] });
    render(<ApiTokenSection projectId="p1" tokens={[token]} />);
    expect(screen.getByText("Read & write")).toBeInTheDocument();
    expect(screen.getByText("Last used: Never")).toBeInTheDocument();
    expect(screen.getByText("Expires: Never")).toBeInTheDocument();
  });

  it("flags a token expiring within 7 days", () => {
    const soon = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString();
    const token = makeToken({ expiresAt: soon });
    render(<ApiTokenSection projectId="p1" tokens={[token]} />);
    expect(screen.getByText("Expires soon")).toBeInTheDocument();
  });

  it("flags an already-expired token", () => {
    const past = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
    const token = makeToken({ expiresAt: past });
    render(<ApiTokenSection projectId="p1" tokens={[token]} />);
    expect(screen.getByText("Expired")).toBeInTheDocument();
  });

  it("issues a token and shows the raw value exactly once in a modal", async () => {
    const issued: ApiTokenWithSecret = { ...makeToken({ name: "New token" }), token: "flt_secretvalue" };
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(issued), { status: 201 }));
    render(<ApiTokenSection projectId="p1" tokens={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Issue token" }));
    const form = screen.getByRole("form", { name: "Issue API token" });
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "New token" } });
    fireEvent.click(within(form).getByRole("button", { name: "Issue token" }));

    expect(await screen.findByText("flt_secretvalue")).toBeInTheDocument();
    expect(screen.getByText("Token issued")).toBeInTheDocument();
    await waitFor(() => expect(refresh).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("flt_secretvalue")).not.toBeInTheDocument();
  });

  it("requires a name before issuing", () => {
    render(<ApiTokenSection projectId="p1" tokens={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Issue token" }));
    const form = screen.getByRole("form", { name: "Issue API token" });
    fireEvent.click(within(form).getByRole("button", { name: "Issue token" }));
    expect(screen.getByText("Name is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("requires a confirmation step before revoking", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<ApiTokenSection projectId="p1" tokens={[makeToken()]} />);

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Confirm revoke" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/api-tokens/token-1"),
      expect.objectContaining({ method: "DELETE" }),
    ));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
  });

  it("shows an inline error when revoke fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "not_found", message: "api token not found" } }), {
        status: 404,
      }),
    );
    render(<ApiTokenSection projectId="p1" tokens={[makeToken()]} />);

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));

    expect(await screen.findByText("api token not found")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
