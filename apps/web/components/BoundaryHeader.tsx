import Link from "next/link";
import { AppIcon } from "./AppIcon";

/**
 * Static header shown on error/not-found boundaries that render before the
 * session is known (or because the page that would have loaded it failed) —
 * same bar as AppHeader, minus the nav and user, which both need a fetched
 * user to render.
 *
 * `leading` mirrors AppHeader's: the project error boundary puts the sidebar
 * toggle there so the collapsed sidebar can still be reopened.
 */
export function BoundaryHeader({ leading }: { leading?: React.ReactNode } = {}) {
  return (
    <header className="border-border bg-card border-b">
      <div className="mx-auto flex max-w-6xl items-center gap-6 px-6 py-3">
        {leading}
        <Link href="/dashboard" className="text-foreground flex items-center gap-2 text-lg font-semibold">
          <AppIcon className="size-6" />
          FlowLens
        </Link>
      </div>
    </header>
  );
}
