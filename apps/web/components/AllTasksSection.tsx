"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { taskPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import type { Priority, Project, TaskStatus, TaskWithProject } from "@/types";
import { PRIORITY_OPTIONS } from "@/lib/priority";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PriorityBadge, PriorityDot } from "@/components/PriorityBadge";
import { TaskSearchBox } from "@/components/TaskSearchBox";
import { TruncatedName } from "@/components/TruncatedName";

type SortValue = "dueOn" | "priority" | "progress" | "updatedAt";

function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <Badge variant={status === "open" ? "default" : "secondary"}>
      {status === "open" ? "Open" : "Closed"}
    </Badge>
  );
}

/**
 * AllTasksSection is the cross-project Task collection at /tasks
 * (docs/ui-design.md, issue #76): every task across every project the user
 * owns, in one list, each still linking to its canonical single view under
 * its own project. Status/priority/sort/project are held in the URL —
 * changing one pushes a new query string, so the server component above
 * re-fetches GET /api/v1/tasks with it, the same round trip the
 * project-scoped TaskListSection makes for its own filters (issue #143).
 * "Only tasks with a due date" is the one purely client-side filter: the API
 * has no such parameter, and the default view's whole point is hiding the
 * undated backlog noise.
 */
export function AllTasksSection({
  tasks,
  projects,
  status,
  priority,
  sort,
  assigneeMe = false,
  search,
  totalCount,
  page = 1,
  perPage = 0,
  nextPage = 0,
  error = false,
}: {
  tasks: TaskWithProject[];
  projects: Project[];
  status: "all" | TaskStatus;
  priority?: Priority;
  sort: SortValue;
  // True when ?assignee=me is set (issue #102): only tasks assigned to the
  // caller's own registered GitLab identity. Held in the URL like the other
  // filters above, not local state, so it survives navigation/refresh.
  assigneeMe?: boolean;
  // The `?q=` the screen was opened with, if any (issue #107) — matched
  // server-side by `websearch_to_tsquery`, the same match the project-scoped
  // TaskListSection's own search box now makes (issue #143).
  search?: string;
  /** How many tasks match the filters across every page, the 1-based page
   *  this one is, its size, and the API's nextPage (0 on the last page).
   *  Omitting them yields no pager, for callers with a single page. */
  totalCount?: number;
  page?: number;
  perPage?: number;
  nextPage?: number;
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [onlyWithDueDate, setOnlyWithDueDate] = useState(true);

  const projectFilter = searchParams.getAll("projectId");

  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  /** changeFilter is updateQuery plus the page reset every filter needs, the
   *  same wrapper the other two paged collections use. */
  function changeFilter(next: Record<string, string | undefined>) {
    updateQuery({ ...next, page: undefined });
  }

  const projectOptions = useMemo(
    () => projects.map((p) => ({ value: p.id, label: p.name })),
    [projects],
  );

  const visible = useMemo(
    () => (onlyWithDueDate ? tasks.filter((t) => t.dueOn) : tasks),
    [tasks, onlyWithDueDate],
  );

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Tasks</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <TaskSearchBox
              value={search ?? ""}
              onChange={(value) => changeFilter({ q: value.trim() === "" ? undefined : value })}
            />
            <Select
              value={status}
              onValueChange={(value) => changeFilter({ status: value === "all" ? undefined : value })}
            >
              <SelectTrigger size="sm" aria-label="Status" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
                <SelectItem value="open">Open</SelectItem>
                <SelectItem value="closed">Closed</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={priority ?? "all"}
              onValueChange={(value) => changeFilter({ priority: value === "all" ? undefined : value })}
            >
              <SelectTrigger size="sm" aria-label="Priority" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All priorities</SelectItem>
                {PRIORITY_OPTIONS.map((option) => (
                  <SelectItem key={option.priority} value={option.priority}>
                    <PriorityDot priority={option.priority} />
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(value) => changeFilter({ sort: value })}>
              <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dueOn">Due date</SelectItem>
                <SelectItem value="priority">Priority</SelectItem>
                <SelectItem value="progress">Progress</SelectItem>
                <SelectItem value="updatedAt">Recently updated</SelectItem>
              </SelectContent>
            </Select>
            <Combobox
              aria-label="Project"
              options={projectOptions}
              value={projectFilter[0] ?? ""}
              onChange={(value) => changeFilter({ projectId: value || undefined })}
              placeholder="All projects"
              searchPlaceholder="Search projects…"
              emptyText="No project found."
              size="sm"
              className="w-44"
            />
            <label className="text-muted-foreground flex items-center gap-1.5 text-sm">
              <input
                type="checkbox"
                checked={onlyWithDueDate}
                onChange={(e) => setOnlyWithDueDate(e.target.checked)}
                className="border-input h-4 w-4 rounded"
              />
              Only with a due date
            </label>
            <label className="text-muted-foreground flex items-center gap-1.5 text-sm">
              <input
                type="checkbox"
                checked={assigneeMe}
                onChange={(e) => changeFilter({ assignee: e.target.checked ? "me" : undefined })}
                className="border-input h-4 w-4 rounded"
              />
              Assigned to me
            </label>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {error ? (
          <p className="text-destructive text-sm">Failed to load tasks. Try refreshing the page.</p>
        ) : tasks.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            {search ? `No tasks match "${search}".` : "No tasks match the current filters."}
          </p>
        ) : visible.length === 0 ? (
          // Checked before the empty-list branch so this hint (rather than the
          // generic "no tasks" one) is what a wide-open filter with nothing
          // dated actually shows.
          <p className="text-muted-foreground text-sm">
            No tasks have a due date. Uncheck &ldquo;Only with a due date&rdquo; to see everything.
          </p>
        ) : (
          <ul className="space-y-2">
            {visible.map((task) => (
              <li key={task.id}>
                <Link
                  href={taskPath(task.projectId, task.id)}
                  className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                >
                  <span className="flex min-w-0 flex-col">
                    <TruncatedName text={task.title} className="text-foreground" />
                    <span className="text-muted-foreground text-xs">{task.projectName}</span>
                  </span>
                  <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                    {task.dueOn ? <span>Due {formatDate(task.dueOn)}</span> : null}
                    <PriorityBadge priority={task.priority} />
                    <StatusBadge status={task.status} />
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
        {/* The pager, shaped like the other two collections'. "Only with a due
            date" is applied in the browser, so on a paged result it narrows
            the page rather than the whole match — the count below is the
            server's, which is what the pager walks. */}
        {!error && (page > 1 || nextPage > 0) ? (
          <nav aria-label="Pagination" className="mt-4 flex items-center justify-between gap-3">
            <p className="text-muted-foreground text-xs">
              {tasks.length === 0 ? 0 : (page - 1) * perPage + 1}–
              {(page - 1) * perPage + tasks.length}
              {totalCount !== undefined ? ` of ${totalCount}` : ""}
            </p>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => updateQuery({ page: page > 2 ? String(page - 1) : undefined })}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={nextPage === 0}
                onClick={() => updateQuery({ page: String(nextPage) })}
              >
                Next
              </Button>
            </div>
          </nav>
        ) : null}
      </CardContent>
    </Card>
  );
}
