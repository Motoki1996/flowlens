import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

/*
 * Renders user-authored long text (a task or backlog description, a task
 * comment, a project description) as GitHub-flavoured Markdown.
 *
 * A description round-trips with a GitLab issue's own description, which is
 * already Markdown, so this is the rendering half of a format the data has
 * always been in. Two consequences follow:
 *
 *   - The text is not fully ours — it can arrive from GitLab. react-markdown
 *     builds a React tree instead of setting innerHTML and drops raw HTML
 *     unless rehype-raw is added, so do NOT add rehype-raw: that is the whole
 *     reason this needs no sanitizer.
 *   - remark-gfm brings GFM autolink literals, which is what turns a pasted
 *     bare URL into a link. It also brings tables, task lists and strikethrough.
 *
 * Styling lives in the `.markdown` block in app/globals.css so it reads from
 * the same theme tokens as everything else.
 */

const SAFE_PROTOCOLS = ["http:", "https:", "mailto:"];

/** Drops anything that isn't a plain link — `javascript:` above all. */
function safeUrl(url: string): string {
  const scheme = /^\s*([a-z][a-z0-9+.-]*):/i.exec(url);
  // No scheme at all: a relative path or a #fragment, which can't navigate away.
  if (!scheme) return url;
  return SAFE_PROTOCOLS.includes(`${scheme[1].toLowerCase()}:`) ? url : "";
}

const components: Components = {
  /*
   * Only the props we name are forwarded — react-markdown also passes its own
   * `node` AST handle, which must not reach the DOM.
   */
  a: ({ children, href, title }) => (
    <a href={href} title={title} target="_blank" rel="noopener noreferrer nofollow">
      {children}
    </a>
  ),
  /*
   * Images are shown as their alt text, not fetched. GitLab stores an issue's
   * attachments as paths relative to the GitLab project (/uploads/...), which
   * FlowLens has no way to resolve, so rendering them would reliably produce a
   * broken image rather than a picture.
   */
  img: ({ alt, src }) => (
    <span className="text-muted-foreground italic">{alt || String(src ?? "image")}</span>
  ),
};

export function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn("markdown", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} urlTransform={safeUrl} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
