"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";

/**
 * The selection + bulk-action machinery shared by the three collection List
 * views (Task, Backlog, Epic). It started as TaskListSection's own (issue
 * #149) and was lifted here once the Backlog and Epic collections wanted the
 * same thing: the pruning rule, the partial-failure wording and the
 * one-action-at-a-time guard are the parts that would quietly drift if each
 * screen kept its own copy.
 *
 * What stays with each screen is what actually differs: which actions it
 * offers, and what request each one sends.
 */

/** requestOk resolves a fetch to whether it succeeded, folding a thrown
 *  network error into the same "this one failed" outcome a non-ok response
 *  produces — so a bulk action's per-object Promise.all never rejects
 *  outright and loses track of which of the others came back fine. */
export async function requestOk(promise: Promise<Response>): Promise<boolean> {
  try {
    return (await promise).ok;
  } catch {
    return false;
  }
}

/**
 * SelectAllCheckbox drives one tri-state checkbox against a set of object ids
 * — checked once every id is selected, indeterminate once some but not all
 * are — so selecting "everything visible" or "everything in this group" is
 * one click instead of one per row. `indeterminate` isn't a DOM attribute
 * React can set via props, so it goes through a ref.
 */
export function SelectAllCheckbox({
  label,
  ids,
  selected,
  onChange,
}: {
  label: string;
  ids: string[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const selectedCount = ids.filter((id) => selected.has(id)).length;
  const allSelected = ids.length > 0 && selectedCount === ids.length;
  const indeterminate = selectedCount > 0 && !allSelected;

  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      checked={allSelected}
      disabled={ids.length === 0}
      onChange={() => {
        const next = new Set(selected);
        for (const id of ids) {
          if (allSelected) next.delete(id);
          else next.add(id);
        }
        onChange(next);
      }}
      className="border-input h-4 w-4 shrink-0 rounded disabled:cursor-not-allowed disabled:opacity-50"
    />
  );
}

/** RowCheckbox is the per-row half of the same selection — its own component
 *  only so the three screens can't disagree on its label shape or styling. */
export function RowCheckbox({
  label,
  id,
  selected,
  onToggle,
}: {
  label: string;
  id: string;
  selected: Set<string>;
  onToggle: (id: string) => void;
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      checked={selected.has(id)}
      onChange={() => onToggle(id)}
      className="border-input h-4 w-4 shrink-0 rounded"
    />
  );
}

export type BulkSelection<A extends string> = {
  selected: Set<string>;
  setSelected: (next: Set<string>) => void;
  toggle: (id: string) => void;
  clear: () => void;
  /** Which action is mid-flight, or null — drives the pending label on its
   *  own button and disables the rest, so two bulk requests never race each
   *  other over the same selection. */
  pending: A | null;
  error: string | null;
  /** Fires one request per selected object and folds the results into a
   *  single outcome: full success clears the selection, a partial failure
   *  reports how many of how many failed and narrows the selection down to
   *  just those, so retrying only resends to the ones that need it.
   *  Resolves to the ids that failed. */
  run: (
    action: A,
    request: (id: string) => Promise<Response>,
  ) => Promise<string[]>;
};

/**
 * useBulkSelection owns the selection for one collection List view.
 *
 * `visibleIds` is what's currently on screen under the active filters: an
 * object selected under one filter can fall out of view under the next, and
 * it's pruned from the selection rather than left as the invisible target of
 * the next bulk action.
 */
export function useBulkSelection<A extends string>({
  visibleIds,
  noun,
  pluralNoun,
}: {
  visibleIds: string[];
  /** The object's name, for the failure wording ("Failed to update 3 backlogs."). */
  noun: string;
  pluralNoun?: string;
}): BulkSelection<A> {
  const router = useRouter();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pending, setPending] = useState<A | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Joined rather than depended on directly: every caller builds this array
  // fresh from its rows each render, so the identity would change every time
  // even when the same objects are on screen.
  const visibleKey = visibleIds.join(",");
  useEffect(() => {
    const visible = new Set(visibleKey === "" ? [] : visibleKey.split(","));
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const next = new Set([...prev].filter((id) => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [visibleKey]);

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const clear = useCallback(() => setSelected(new Set()), []);

  const plural = pluralNoun ?? `${noun}s`;

  const run = useCallback(
    async (
      action: A,
      request: (id: string) => Promise<Response>,
    ): Promise<string[]> => {
      const ids = Array.from(selected);
      if (ids.length === 0) return [];
      setPending(action);
      setError(null);
      try {
        const results = await Promise.all(
          ids.map(async (id) => ({ id, ok: await requestOk(request(id)) })),
        );
        const failed = results.filter((r) => !r.ok).map((r) => r.id);
        if (failed.length > 0) {
          setError(
            failed.length === ids.length
              ? `Failed to update ${ids.length} ${ids.length === 1 ? noun : plural}.`
              : `${failed.length} of ${ids.length} ${plural} failed to update.`,
          );
          setSelected(new Set(failed));
        } else {
          setSelected(new Set());
        }
        router.refresh();
        return failed;
      } finally {
        setPending(null);
      }
    },
    [selected, noun, plural, router],
  );

  return useMemo(
    () => ({ selected, setSelected, toggle, clear, pending, error, run }),
    [selected, toggle, clear, pending, error, run],
  );
}

/** The action names BulkActionBar owns; a screen's own extra control (the
 *  Task collection's "Assign to backlog", the Epic collection's "Move to
 *  backlog") names its own on top of these. */
export type BaseBulkAction = "priority" | "progress" | "close" | "reopen";
