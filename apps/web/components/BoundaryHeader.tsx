import Link from "next/link";
import { AppIcon } from "./AppIcon";

/**
 * Static header shown on error/not-found boundaries that render before the
 * session is known (or because the page that would have loaded it failed) —
 * same bar as AppHeader, minus the nav and user, which both need a fetched
 * user to render.
 */
export function BoundaryHeader() {
  return (
    <header className="border-border bg-card border-b">
      <div className="mx-auto flex max-w-6xl items-center px-6 py-3">
        <Link href="/dashboard" className="text-foreground flex items-center gap-2 text-lg font-semibold">
          <AppIcon className="size-6" />
          FlowLens
        </Link>
      </div>
    </header>
  );
}
