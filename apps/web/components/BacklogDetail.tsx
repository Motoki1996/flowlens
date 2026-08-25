"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Plus } from "lucide-react";
import type { Backlog, Epic, LinkedGitlabProject, Task, TaskStatus } from "@/types";
import { backlogsPath, epicPath, epicsPath, taskPath, tasksPath } from "@/lib/routes";
import { backlogCompletion, groupTaskCompletion } from "@/lib/timeline";
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
import { BacklogEditForm } from "@/components/BacklogEditForm";
import { BacklogDeleteButton } from "@/components/BacklogDeleteButton";
import { EpicForm } from "@/components/EpicForm";
import { MetricTabs } from "@/components/MetricTabs";

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

/**
 * BacklogDetail is the single view for one backlog: identity, attributes,
 * then its tasks, per docs/ui-design.md. Editing happens here, on the object
 * it acts on (rule 4) — the same form the collection view's list rows use, so
 * the two screens can never drift apart on what a backlog's fields are. There
 * is no /backlogs/[id]/edit route. Delete lives here too, and hands back to
 * the collection once there is no object left to show. The link back to the
 * collection is the page's breadcrumb, so it is not repeated inside this
 * component.
 */
export function BacklogDetail({
  backlog: initialBacklog,
  project,
  epics = [],
  tasks = [],
  links = [],
  tasksError = false,
}: {
  backlog: Backlog;
  project: { id: string; name: string };
  /** This backlog's epics (the optional rung, ADR-0012). Empty is a normal
   *  state — a backlog broken straight down into tasks — and only decides
   *  which tab opens first; the Epics tab itself is always present, since
   *  it is where the first epic gets created. */
  epics?: Epic[];
  tasks?: Task[];
  /** The project's linked GitLab projects (issue #180), used to name this
   *  backlog's issue destination. Empty hides that row. */
  links?: LinkedGitlabProject[];
  tasksError?: boolean;
}) {
  const router = useRouter();
  const [backlog, setBacklog] = useState(initialBacklog);
  const [editing, setEditing] = useState(false);
  // Epics open first when this backlog uses them, tasks otherwise: a backlog
  // that hasn't adopted the rung shouldn't have it pushed in front of the
  // work it actually holds.
  const [tab, setTab] = useState<"epics" | "tasks">(epics.length > 0 ? "epics" : "tasks");
  const [creatingEpic, setCreatingEpic] = useState(false);
  // A save writes the API's response straight into state so the card reads
  // back the saved values without waiting for the server round trip. The
  // page around it (the breadcrumb names the backlog too) still needs the
  // refresh, and when that arrives this adopts the server's value rather
  // than holding the older one — React's own "adjusting state on a prop
  // change" pattern.
  const [renderedFrom, setRenderedFrom] = useState(initialBacklog);
  if (renderedFrom !== initialBacklog) {
    setRenderedFrom(initialBacklog);
    setBacklog(initialBacklog);
  }

  // tasks is already filtered to this backlog by the page, but backlogCompletion
  // is the one place the ratio is defined, so it counts rather than the view.
  const completion = backlogCompletion(tasks, backlog.id);

  // The same resolution internal/task.Service.Create applies when a task is
  // filed here: this backlog's own link first, then the project's default.
  // `inherited` is what distinguishes the two for the reader — the value is
  // the same GitLab project either way, but only one of them follows the
  // project if its default changes.
  const issueDestination = (() => {
    const own = backlog.defaultLinkedGitlabProjectId
      ? links.find((l) => l.id === backlog.defaultLinkedGitlabProjectId)
      : undefined;
    if (own) return { link: own, inherited: false };
    const projectDefault = links.find((l) => l.isDefault);
    return projectDefault ? { link: projectDefault, inherited: true } : null;
  })();

  return (
    <>
      <Card>
        {editing ? (
          <CardContent>
            <BacklogEditForm
              backlog={backlog}
              links={links}
              onSaved={(updated) => {
                setBacklog(updated);
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
                    {backlog.name}
                  </h1>
                  <CardDescription className="mt-1.5">
                    {backlog.description ? (
                      <Markdown>{backlog.description}</Markdown>
                    ) : (
                      "No description"
                    )}
                  </CardDescription>
                </div>
                <div className="flex shrink-0 items-start gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setEditing(true)}
                  >
                    Edit backlog
                  </Button>
                  {/* Deleting leaves this screen with no object to show, so
                      it hands back to the Backlog collection — the tasks it
                      kept are still there, under Unclassified. */}
                  <BacklogDeleteButton
                    backlog={backlog}
                    onDeleted={() => {
                      router.push(backlogsPath(project.id));
                      router.refresh();
                    }}
                  />
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">Priority</dt>
                  <dd className="text-foreground">
                    <PriorityBadge priority={backlog.priority} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Progress</dt>
                  <dd className="text-foreground">
                    <ProgressBadge progress={backlog.progress} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Start date</dt>
                  <dd className="text-foreground">
                    {backlog.startDate
                      ? formatDate(backlog.startDate)
                      : "Not set"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Due date</dt>
                  <dd className="text-foreground">
                    {backlog.dueOn ? formatDate(backlog.dueOn) : "Not set"}
                  </dd>
                </div>
                {/* The same closed/total ratio the Backlog timeline fills its bar
                with, stated here as the number it is. */}
                <div>
                  <dt className="text-muted-foreground">Completed tasks</dt>
                  <dd className="text-foreground">
                    {tasksError
                      ? "Unavailable"
                      : completion.total === 0
                        ? "No tasks"
                        : `${completion.closed}/${completion.total} closed (${Math.round(completion.ratio * 100)}%)`}
                  </dd>
                </div>
                {/* Where a task filed here gets its GitLab issue created (issue
                #180). Hidden entirely for a project with no linked GitLab
                project — there is no destination to state. */}
                {links.length > 0 ? (
                  <div>
                    <dt className="text-muted-foreground">
                      GitLab project for new issues
                    </dt>
                    <dd className="text-foreground">
                      {issueDestination ? (
                        <>
                          {issueDestination.link.pathWithNamespace}
                          {issueDestination.inherited ? (
                            <span className="text-muted-foreground">
                              {" "}
                              (project default)
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
                    {backlog.baseBranch ? (
                      <code>{backlog.baseBranch}</code>
                    ) : (
                      "Not set"
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Allowed scope</dt>
                  <dd className="text-foreground whitespace-pre-wrap">
                    {backlog.allowedScope || "Not set"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Forbidden scope</dt>
                  <dd className="text-foreground whitespace-pre-wrap">
                    {backlog.forbiddenScope || "Not set"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Created</dt>
                  <dd className="text-foreground">
                    {formatDateTime(backlog.createdAt)}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Updated</dt>
                  <dd className="text-foreground">
                    {formatDateTime(backlog.updatedAt)}
                  </dd>
                </div>
              </dl>
            </CardContent>
          </>
        )}
      </Card>

      {/* One card, two tabs, rather than an Epics list stacked on a Tasks
          list: both are this backlog's children, and showing them at once
          made the screen twice as tall for no gain — only one of them is
          being read at a time. The tabs carry counts so the hidden side
          still reports its size, and the tab labels stand in for a card
          title: each one names the object it shows, which no invented noun
          ("Contents") would have done as well.

          The choice is deliberately local state, not the URL: nothing is
          refetched by it, so it is a reading preference on data the page
          already has — the same reasoning MetricTabs' own doc comment
          gives. */}
      <Card className="mt-8">
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <MetricTabs
              label="Contents"
              tabs={[
                { key: "epics", label: `Epics ${epics.length}` },
                { key: "tasks", label: `Tasks ${tasks.length}` },
              ]}
              value={tab}
              onChange={(next) => {
                setTab(next);
                // Leaving the tab abandons a half-written epic rather than
                // hiding a form that would reappear later out of context.
                setCreatingEpic(false);
              }}
            />
            <div className="flex flex-wrap items-center gap-3">
              {tab === "epics" && !creatingEpic ? (
                <Button variant="outline" size="sm" onClick={() => setCreatingEpic(true)}>
                  <Plus className="size-4" aria-hidden />
                  New epic
                </Button>
              ) : null}
              {/* The lists below are read-only previews; filtering, the
                  board/timeline modes and (for tasks) creation all belong to
                  the collection that owns the object, so this hands off
                  rather than duplicating them. */}
              <Link
                href={
                  tab === "epics"
                    ? `${epicsPath(project.id)}?backlog=${backlog.id}`
                    : tasksPath(project.id, { backlogId: backlog.id })
                }
                className="text-muted-foreground hover:text-foreground text-sm hover:underline"
              >
                {tab === "epics" ? "Open in Epics" : "Open in Tasks"}
              </Link>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {tab === "epics" ? (
            <>
              {creatingEpic ? (
                <div className="mb-4">
                  {/* The parent is this backlog, so the form's own backlog
                      picker has exactly one thing to offer — the field stays
                      rather than being special-cased away, so the created
                      epic still states where it landed. */}
                  <EpicForm
                    projectId={project.id}
                    backlogs={[backlog]}
                    links={links}
                    defaultBacklogId={backlog.id}
                    onSaved={() => {
                      router.refresh();
                      setCreatingEpic(false);
                    }}
                    onCancel={() => setCreatingEpic(false)}
                  />
                </div>
              ) : null}
              {epics.length === 0 ? (
                <p className="text-muted-foreground text-sm">
                  No epics in this backlog. Tasks can sit in it directly — an epic is
                  the optional middle rung, for when the work is worth cutting into
                  coarser units first.
                </p>
              ) : (
                <ul className="space-y-2">
                  {epics.map((epic) => {
                    const epicCompletion = groupTaskCompletion(epic);
                    return (
                      <li key={epic.id}>
                        <Link
                          href={epicPath(project.id, epic.id)}
                          className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                        >
                          <span className="text-foreground">{epic.name}</span>
                          <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                            {epic.baseBranch ? <code>{epic.baseBranch}</code> : null}
                            <span className="tabular-nums">
                              {epicCompletion.total === 0
                                ? "No tasks"
                                : `${epicCompletion.closed}/${epicCompletion.total} closed`}
                            </span>
                            <ProgressBadge progress={epic.progress} />
                          </span>
                        </Link>
                      </li>
                    );
                  })}
                </ul>
              )}
            </>
          ) : tasksError ? (
            <p className="text-destructive text-sm">
              Failed to load tasks. Try refreshing the page.
            </p>
          ) : tasks.length === 0 ? (
            <p className="text-muted-foreground text-sm">No tasks in this backlog yet.</p>
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
