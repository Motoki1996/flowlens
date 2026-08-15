"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { ViewMode } from "@/components/ViewModeToggle";

const DEFAULT_VIEW: ViewMode = "board";

/**
 * useViewMode holds the Board/List/Timeline toggle (ViewModeToggle) shared by
 * TaskListSection and BacklogListSection, synced with `?view=` the way every
 * other filter on those two screens already is (issue #153): sharing the URL,
 * reloading, or navigating back all land on the view the sender was looking
 * at. Board is the default and drops the param back out of the query string,
 * mirroring the changeXFilter functions in those two components.
 *
 * `initialView` is the page's own already-validated `?view=` (see
 * tasks/page.tsx and backlogs/page.tsx — the same fallback-to-default
 * treatment `initialSort`/`initialProgressFilter` already get). The value
 * returned here stays local state rather than reading straight off
 * `useSearchParams`, unlike TaskListSection's own `?label=`/`?due=`: those
 * narrow data already sitting in memory, so waiting on a re-render is fine,
 * but switching views has to feel instant. The effect below is what keeps
 * that local state in step whenever `initialView` changes underneath it —
 * a real navigation, such as the back button.
 */
export function useViewMode(initialView: ViewMode) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [view, setView] = useState(initialView);
  useEffect(() => setView(initialView), [initialView]);

  function changeView(next: ViewMode) {
    setView(next);
    const params = new URLSearchParams(searchParams.toString());
    params.delete("view");
    if (next !== DEFAULT_VIEW) params.set("view", next);
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  return [view, changeView] as const;
}
