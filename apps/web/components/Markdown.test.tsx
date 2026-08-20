import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Markdown } from "./Markdown";

/*
 * The rendering contract for every user-authored long text in the app. The
 * call sites (TaskDetail, BacklogDetail, ProjectDetail, TaskActivitySection)
 * deliberately don't re-test any of this — see docs/testing.md.
 */
describe("Markdown", () => {
  it("renders GFM: headings, lists and inline formatting", () => {
    render(<Markdown>{"# Title\n\n- one\n- **two**"}</Markdown>);
    expect(screen.getByRole("heading", { name: "Title" })).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("two").tagName).toBe("STRONG");
  });

  it("turns a pasted bare URL into a link that opens safely in a new tab", () => {
    render(
      <Markdown>
        {"See https://gitlab.example.com/group/demo/-/issues/7 for detail."}
      </Markdown>,
    );
    const link = screen.getByRole("link", {
      name: "https://gitlab.example.com/group/demo/-/issues/7",
    });
    expect(link).toHaveAttribute(
      "href",
      "https://gitlab.example.com/group/demo/-/issues/7",
    );
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer nofollow");
  });

  it("does not execute raw HTML — a description can come from GitLab", () => {
    const { container } = render(
      <Markdown>{'<img src=x onerror="alert(1)"><b>bold</b>'}</Markdown>,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("b")).toBeNull();
    expect(container.textContent).toContain("<b>bold</b>");
  });

  it("strips a javascript: link but keeps its text", () => {
    const { container } = render(
      <Markdown>{"[click me](javascript:alert(1))"}</Markdown>,
    );
    expect(screen.getByText("click me")).toBeInTheDocument();
    expect(container.querySelector("a")?.getAttribute("href")).toBe("");
  });

  it("shows an image as its alt text rather than fetching a GitLab-relative path", () => {
    const { container } = render(
      <Markdown>{"![a screenshot](/uploads/abc/shot.png)"}</Markdown>,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("a screenshot")).toBeInTheDocument();
  });
});
