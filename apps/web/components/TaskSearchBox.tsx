"use client";

import { useEffect, useRef, useState } from "react";
import { Input } from "@/components/ui/input";

// 300ms: long enough to absorb normal typing speed, short enough that the
// cross-project screen's server round trip (issue #107) still feels live.
const DEBOUNCE_MS = 300;

/**
 * TaskSearchBox is the search input shared by both Task collection screens
 * (the project-scoped List/Board/Timeline at /projects/[projectId]/tasks and
 * the cross-project list at /tasks), placed next to their other filters the
 * same way ViewModeToggle.tsx sits next to the view switch. The input itself
 * updates every keystroke so typing feels responsive; onChange — which drives
 * a URL update and, on the cross-project screen, a server refetch — fires
 * debounced so neither screen does that work per keystroke.
 *
 * `label` (issue #151) lets the Backlog collection reuse the same debounced
 * box for its own, client-side-only name search rather than duplicating it —
 * it names what's being searched ("tasks"/"backlogs") in both the aria-label
 * and the placeholder, and defaults to "tasks" so the two existing callers
 * are unaffected.
 */
export function TaskSearchBox({
  value,
  onChange,
  className,
  label = "tasks",
}: {
  value: string;
  onChange: (value: string) => void;
  className?: string;
  label?: string;
}) {
  const [draft, setDraft] = useState(value);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Keeps the input in sync when `value` changes from outside (e.g. the
  // browser's back button restoring an earlier `?q=`).
  useEffect(() => setDraft(value), [value]);

  useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  function handleChange(next: string) {
    setDraft(next);
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(() => onChange(next), DEBOUNCE_MS);
  }

  return (
    <Input
      aria-label={`Search ${label}`}
      placeholder={`Search ${label}…`}
      value={draft}
      onChange={(e) => handleChange(e.target.value)}
      className={className ?? "h-8 w-40"}
    />
  );
}
