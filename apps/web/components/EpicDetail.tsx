"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Backlog, Epic, LinkedGitlabProject, Task, TaskStatus } from "@/types";
import { backlogPath, epicsPath, taskPath, tasksPath } from "@/lib/routes";
import { groupTaskCompletion } from "@/lib/timeline";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";
import { Markdown } from "@/components/Markdown";
import { Button } from "@/components/ui/button";
import { EpicForm } from "@/components/EpicForm";
import { EpicDeleteButton } from "@/components/EpicDeleteButton";

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <Badge variant={status === "open" ? "default" : "secondary"}>
      {status === "open" ? "Open" : "Closed"}
    </Badge>
  );
}

/** Renders one inherited-or-own field: an epic that sets nothing falls
 *  through to its backlog, and the reader has to be able to tell which of the
 *  two they're looking at, since only one of them follows the backlog when it
 *  changes. The chain runs per field (internal/task's defaultsForTask), so
 *  each of these is answered independently. */
function InheritedValue({
  own,
  inherited,
  mono = false,
}: {
  own: string;
  inherited: string;
  mono?: boolean;
}) {
  const value = own || inherited;
  if (!value) return <>Not set</>;
  return (
    <>
      {mono ? <code>{value}</code> : value}
      {own ? null : <span className="text-muted-foreground"> (from backlog)</span>}
    </>
  );
}

/**
 * EpicDetail is the single view for one epic: identity, attributes, then its
 * tasks — the same shape BacklogDetail has, since an epic is a backlog that
 * lives inside a backlog (ADR-0012). Editing and delete happen here, on the
 * object they act on (docs/ui-design.md rule 4); there is no
 * /epics/[id]/edit route.
 *
 * What this screen has that BacklogDetail doesn't is the inheritance: base
 * branch and allowed/forbidden scope fall through to the epic's backlog when
 * this epic leaves them empty, and the reader is told which they are seeing.
 */
export function EpicDetail({
  epic: initialEpic,
  project,
  backlog = null,
  backlogs = [],
  tasks = [],
  links = [],
  tasksError = false,
}: {
  epic: Epic;
  project: { id: string; name: string };
  /** The epic's own backlog, or null when it is unfiled. Its base branch and
   *  scope are what this epic inherits when it sets none of its own. */
  backlog?: Backlog | null;
  /** Every backlog in the project, for the edit form's parent picker. */
  backlogs?: Backlog[];
  tasks?: Task[];
  /** The project's linked GitLab projects, used to name this epic's issue
   *  destination. Empty hides that row. */
  links?: LinkedGitlabProject[];
  tasksError?: boolean;
}) {
  const router = useRouter();
  const [epic, setEpic] = useState(initialEpic);
  const [editing, setEditing] = useState(false);
  // A save writes the API's response straight into state so the card reads
  // back the saved values without waiting for the server round trip; when the
  // refresh arrives this adopts the server's value rather than holding the
  // older one — the same arrangement BacklogDetail uses.
  const [renderedFrom, setRenderedFrom] = useState(initialEpic);
  if (renderedFrom !== initialEpic) {
    setRenderedFrom(initialEpic);
    setEpic(initialEpic);
  }

  const completion = groupTaskCompletion(epic);

  // The same resolution internal/task.Service.Create applies to a task filed
  // here: this epic's own link, then its backlog's, then the project's
  // default. `from` is what distinguishes them for the reader.
  const issueDestination = (() => {
    const own = epic.defaultLinkedGitlabProjectId
      ? links.find((l) => l.id === epic.defaultLinkedGitlabProjectId)
      : undefined;
    if (own) return { link: own, from: null };
    const fromBacklog = backlog?.defaultLinkedGitlabProjectId
      ? links.find((l) => l.id === backlog.defaultLinkedGitlabProjectId)
      : undefined;
    if (fromBacklog) return { link: fromBacklog, from: "backlog" };
    const projectDefault = links.find((l) => l.isDefault);
    return projectDefault ? { link: projectDefault, from: "project default" } : null;
  })();

  return (
    <>
      <Card>
        {editing ? (
          <CardContent>
            <EpicForm
              projectId={project.id}
              epic={epic}
              backlogs={backlogs}
              links={links}
              onSaved={(updated) => {
                setEpic(updated);
                setEditing(false);
                router.refresh();
              }}
              onCancel={() => setEditing(false)}
            />
          </CardContent>
        ) : (
          <>
            <CardHeader>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <h1 className="text-foreground text-xl leading-none font-semibold">
                    {epic.name}
                  </h1>
                  <CardDescription className="mt-1.5">
                    {epic.description ? (
                      <Markdown>{epic.description}</Markdown>
                    ) : (
                      "No description"
                    )}
                  </CardDescription>
                </div>
                <div className="flex shrink-0 items-start gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                    Edit epic
                  </Button>
                  {/* Deleting leaves this screen with no object to show, so it
                      hands back to the Epic collection — the tasks it kept are
                      still there, in their backlog. */}
                  <EpicDeleteButton
                    epic={epic}
                    onDeleted={() => {
                      router.push(epicsPath(project.id));
                      router.refresh();
                    }}
                  />
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">Backlog</dt>
                  <dd className="text-foreground">
                    {backlog ? (
                      <Link
                        href={backlogPath(project.id, backlog.id)}
                        className="hover:underline"
                      >
                        {backlog.name}
                      </Link>
                    ) : (
                      "No backlog"
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Priority</dt>
                  <dd className="text-foreground">
                    <PriorityBadge priority={epic.priority} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Progress</dt>
                  <dd className="text-foreground">
                    <ProgressBadge progress={epic.progress} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Start date</dt>
                  <dd className="text-foreground">
                    {epic.startDate ? formatDate(epic.startDate) : "Not set"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Due date</dt>
                  <dd className="text-foreground">
                    {epic.dueOn ? formatDate(epic.dueOn) : "Not set"}
                  </dd>
                </div>
                {/* Read off the epic's own aggregated counts, the same number
                    the collection's bars are filled by. */}
                <div>
                  <dt className="text-muted-foreground">Completed tasks</dt>
                  <dd className="text-foreground">
                    {completion.total === 0
                      ? "No tasks"
                      : `${completion.closed}/${completion.total} closed (${Math.round(
                          completion.ratio * 100,
                        )}%)`}
                  </dd>
                </div>
                {links.length > 0 ? (
                  <div>
                    <dt className="text-muted-foreground">GitLab project for new issues</dt>
                    <dd className="text-foreground">
                      {issueDestination ? (
                        <>
                          {issueDestination.link.pathWithNamespace}
                          {issueDestination.from ? (
                            <span className="text-muted-foreground">
                              {" "}
                              ({issueDestination.from})
                            </span>
                          ) : null}
                        </>
                      ) : (
                        "Not set"
                      )}
                    </dd>
                  </div>
                ) : null}
                <div>
                  <dt className="text-muted-foreground">Base branch</dt>
                  <dd className="text-foreground">
                    <InheritedValue
                      own={epic.baseBranch}
                      inherited={backlog?.baseBranch ?? ""}
                      mono
                    />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Allowed scope</dt>
                  <dd className="text-foreground whitespace-pre-wrap">
                    <InheritedValue
                      own={epic.allowedScope}
                      inherited={backlog?.allowedScope ?? ""}
                    />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Forbidden scope</dt>
                  <dd className="text-foreground whitespace-pre-wrap">
                    <InheritedValue
                      own={epic.forbiddenScope}
                      inherited={backlog?.forbiddenScope ?? ""}
                    />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Created</dt>
                  <dd className="text-foreground">{formatDateTime(epic.createdAt)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Updated</dt>
                  <dd className="text-foreground">{formatDateTime(epic.updatedAt)}</dd>
                </div>
              </dl>
            </CardContent>
          </>
        )}
      </Card>

      <Card className="mt-8">
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <CardTitle className="text-base font-medium">Tasks</CardTitle>
            {/* The list below is a read-only preview; filtering, the timeline
                view and task creation all belong to the Task collection, so
                this link hands off rather than duplicating them. */}
            <Link
              href={tasksPath(project.id, { epicId: epic.id })}
              className="text-muted-foreground hover:text-foreground text-sm hover:underline"
            >
              Open in Tasks
            </Link>
          </div>
        </CardHeader>
        <CardContent>
          {tasksError ? (
            <p className="text-destructive text-sm">
              Failed to load tasks. Try refreshing the page.
            </p>
          ) : tasks.length === 0 ? (
            <p className="text-muted-foreground text-sm">No tasks in this epic yet.</p>
          ) : (
            <ul className="space-y-2">
              {tasks.map((task) => (
                <li key={task.id}>
                  <Link
                    href={taskPath(project.id, task.id)}
                    className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                  >
                    <span className="text-foreground">{task.title}</span>
                    <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                      {task.assigneeGitlabUsername ? (
                        <span>{task.assigneeGitlabUsername}</span>
                      ) : null}
                      {task.dueOn ? <span>Due {formatDate(task.dueOn)}</span> : null}
                      <StatusBadge status={task.status} />
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </>
  );
}
