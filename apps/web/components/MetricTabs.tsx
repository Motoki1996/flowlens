"use client";

import { useRef } from "react";

/**
 * MetricTabs is the small segmented control the metrics cards use to switch
 * how an already-fetched series is read — Median/p90 on Delivery metrics
 * (issue #189), Tasks/Points on Velocity. It is deliberately not a URL
 * filter: nothing is refetched, so the choice is a view preference on data
 * the server already sent, unlike ?from=/?to=/?interval=.
 *
 * A small hand-rolled tablist (role="tablist"/"tab"/aria-selected, arrow-key
 * roving tabindex) rather than @radix-ui/react-tabs, which isn't a dependency
 * of this repo yet.
 */
export function MetricTabs<T extends string>({
  label,
  tabs,
  value,
  onChange,
}: {
  /** Names the group for screen readers, e.g. "Statistic" or "Unit". */
  label: string;
  tabs: ReadonlyArray<{ key: T; label: string }>;
  value: T;
  onChange: (next: T) => void;
}) {
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);

  function handleKeyDown(event: React.KeyboardEvent<HTMLButtonElement>, index: number) {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const next =
      event.key === "ArrowRight" ? (index + 1) % tabs.length : (index - 1 + tabs.length) % tabs.length;
    onChange(tabs[next].key);
    tabRefs.current[next]?.focus();
  }

  return (
    <div role="tablist" aria-label={label} className="border-border inline-flex gap-0.5 rounded-md border p-0.5">
      {tabs.map((tab, index) => (
        <button
          key={tab.key}
          ref={(el) => {
            tabRefs.current[index] = el;
          }}
          type="button"
          role="tab"
          aria-selected={value === tab.key}
          tabIndex={value === tab.key ? 0 : -1}
          onClick={() => onChange(tab.key)}
          onKeyDown={(event) => handleKeyDown(event, index)}
          className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
            value === tab.key ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}
