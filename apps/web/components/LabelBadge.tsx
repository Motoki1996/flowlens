"use client";

import type { MouseEvent } from "react";
import { Badge } from "@/components/ui/badge";

/**
 * LabelBadge renders one of a task's GitLab labels as a filter toggle (issue
 * #147): clicking it sets the Task collection's `?label=` to this label, or
 * clears it if it was already the active one. It sits inside a task's title
 * Link (List mode row) or its board card (Board mode), both of which treat a
 * click anywhere else as opening the task — so a click here must not bubble
 * into that navigation.
 *
 * A real `button` (rather than a `span` with a click handler) is what makes
 * this reachable and operable by keyboard for free.
 */
export function LabelBadge({
  label,
  active,
  onToggle,
}: {
  label: string;
  active: boolean;
  onToggle: (label: string) => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={(e: MouseEvent<HTMLButtonElement>) => {
        e.preventDefault();
        e.stopPropagation();
        onToggle(label);
      }}
      className="focus-visible:ring-ring rounded-full focus-visible:ring-2 focus-visible:outline-none"
    >
      <Badge variant={active ? "default" : "outline"}>{label}</Badge>
    </button>
  );
}
