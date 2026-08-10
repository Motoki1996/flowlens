"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { taskPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import type { Priority, Project, TaskStatus, TaskWithProject } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PriorityBadge } from "@/components/PriorityBadge";

type SortValue = "dueOn" | "priority" | "updatedAt";

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
 * re-fetches GET /api/v1/tasks with it — unlike the project-scoped
 * TaskListSection, which already has every one of its project's tasks in
 * hand and filters client-side. "Only tasks with a due date" is the one
 * purely client-side filter: the API has no such parameter, and the default
 * view's whole point is hiding the undated backlog noise.
 */
export function AllTasksSection({
  tasks,
  projects,
  status,
  priority,
  sort,
  assigneeMe = false,
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
            <Select
              value={status}
              onValueChange={(value) => updateQuery({ status: value === "all" ? undefined : value })}
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
              onValueChange={(value) => updateQuery({ priority: value === "all" ? undefined : value })}
            >
              <SelectTrigger size="sm" aria-label="Priority" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All priorities</SelectItem>
                <SelectItem value="low">Low</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="high">High</SelectItem>
                <SelectItem value="urgent">Urgent</SelectItem>
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(value) => updateQuery({ sort: value })}>
              <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dueOn">Due date</SelectItem>
                <SelectItem value="priority">Priority</SelectItem>
                <SelectItem value="updatedAt">Recently updated</SelectItem>
              </SelectContent>
            </Select>
            <Combobox
              aria-label="Project"
              options={projectOptions}
              value={projectFilter[0] ?? ""}
              onChange={(value) => updateQuery({ projectId: value || undefined })}
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
                onChange={(e) => updateQuery({ assignee: e.target.checked ? "me" : undefined })}
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
          <p className="text-muted-foreground text-sm">No tasks match the current filters.</p>
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
                    <span className="text-foreground truncate">{task.title}</span>
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
      </CardContent>
    </Card>
  );
}
