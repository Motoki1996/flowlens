"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { backlogsPath, gitlabConnectionPath, tasksPath } from "@/lib/routes";
import type {
  ApiError,
  ApiToken,
  DeliveryMetrics,
  FlowMetrics,
  MetricsInterval,
  GitlabConnection,
  Project,
  ProjectInvite,
  ProjectMember,
  SyncJob,
  Velocity,
} from "@/types";
import { Card, CardHeader, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ApiTokenSection } from "@/components/ApiTokenSection";
import { DeliveryMetricsSection } from "@/components/DeliveryMetricsSection";
import { FailedSyncJobSection } from "@/components/FailedSyncJobSection";
import { ProjectInviteSection } from "@/components/ProjectInviteSection";
import { ProjectMemberSection } from "@/components/ProjectMemberSection";
import { VelocitySection } from "@/components/VelocitySection";

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** EditProjectForm is the inline edit form shown in place of the identity block. */
function EditProjectForm({
  project,
  onSaved,
  onCancel,
}: {
  project: Project;
  onSaved: (project: Project) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Project name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${project.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ name, description }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to update project.");
        return;
      }
      onSaved((await res.json()) as Project);
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" aria-label="Edit project">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div>
        <label htmlFor="edit-project-name" className="text-foreground block text-sm font-medium">
          Name
        </label>
        <Input
          id="edit-project-name"
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <label htmlFor="edit-project-description" className="text-foreground block text-sm font-medium">
          Description
        </label>
        <Textarea
          id="edit-project-description"
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** DeleteProjectButton interposes an inline confirmation before deleting. */
function DeleteProjectButton({ project }: { project: Project }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${project.id}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete project.");
        setPending(false);
        return;
      }
      router.push("/projects");
      router.refresh();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex items-center gap-2">
        {error ? <span className="text-destructive text-sm">{error}</span> : null}
        <span className="text-foreground text-sm">Delete this project?</span>
        <Button variant="destructive" size="sm" onClick={handleDelete} disabled={pending}>
          {pending ? "Deleting…" : "Confirm delete"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
          Cancel
        </Button>
      </div>
    );
  }

  return (
    <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
      Delete
    </Button>
  );
}

/** gitlabSummary reads the connection the way the connection screen would: a
 *  broken connection is worth surfacing here, an intact one is just its size. */
function gitlabSummary(connection: GitlabConnection | null, linkedProjectCount: number) {
  if (!connection) return "Not connected";
  if (connection.lastVerifyError) return "Connection invalid";
  return `${linkedProjectCount} linked projects`;
}

/** CollectionLink is one entry in the project's related-collections block. */
function CollectionLink({
  href,
  name,
  summary,
}: {
  href: string;
  name: string;
  summary: string;
}) {
  return (
    <Link
      href={href}
      className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-4 py-3 transition-colors"
    >
      <span className="text-foreground text-sm font-medium">{name}</span>
      <span className="text-muted-foreground text-sm">{summary}</span>
    </Link>
  );
}

/**
 * ProjectDetail is the single view for one project. Attribute order is
 * fixed per docs/ui-design.md: identity -> attributes -> related
 * collections. Backlogs and tasks are collections of their own and get their
 * own screens, so they appear here as links with a count rather than as
 * embedded lists. Edit and delete actions live here rather than on a separate
 * "manage" screen.
 */
export function ProjectDetail({
  project: initial,
  backlogCount = 0,
  taskCount = 0,
  openTaskCount = 0,
  countsError = false,
  gitlabConnection = null,
  linkedProjectCount = 0,
  apiTokens = [],
  failedSyncJobs = [],
  members = null,
  invites = null,
  currentUserId,
  metrics = null,
  flowMetrics = null,
  velocity = null,
  metricsError = false,
  velocityError = false,
  metricsFrom,
  metricsTo,
  metricsInterval,
}: {
  project: Project;
  backlogCount?: number;
  taskCount?: number;
  openTaskCount?: number;
  /** True when the counts could not be loaded; the links still work. */
  countsError?: boolean;
  gitlabConnection?: GitlabConnection | null;
  linkedProjectCount?: number;
  apiTokens?: ApiToken[];
  failedSyncJobs?: SyncJob[];
  /** null when the caller isn't a project owner (the listing is owner-only). */
  members?: ProjectMember[] | null;
  /** null when the caller isn't a project owner, in which case the invites
   *  card is hidden entirely (issue #211). */
  invites?: ProjectInvite[] | null;
  /** The viewer's own user ID, so their member row can hide its controls. */
  currentUserId: string;
  /** Delivery-flow metrics (issue #113); null when they failed to load. */
  metrics?: DeliveryMetrics | null;
  /** Per-task stage lead-time metrics (issue #171); null when they failed
   *  to load. */
  flowMetrics?: FlowMetrics | null;
  /** Completed-task throughput (issue #195); null when it failed to load. */
  velocity?: Velocity | null;
  metricsError?: boolean;
  velocityError?: boolean;
  metricsFrom?: string;
  metricsTo?: string;
  metricsInterval?: MetricsInterval;
}) {
  const [project, setProject] = useState(initial);
  const [editing, setEditing] = useState(false);

  return (
    <>
      <Card>
        <CardHeader>
          {editing ? (
            <EditProjectForm
              project={project}
              onSaved={(updated) => {
                setProject(updated);
                setEditing(false);
              }}
              onCancel={() => setEditing(false)}
            />
          ) : (
            <>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h1 className="text-foreground text-xl leading-none font-semibold">
                    {project.name}
                  </h1>
                  <CardDescription className="mt-1.5">
                    {project.description || "No description"}
                  </CardDescription>
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                    Edit
                  </Button>
                  <DeleteProjectButton project={project} />
                </div>
              </div>
            </>
          )}
        </CardHeader>
        <CardContent>
          {project.failedSyncTaskCount > 0 ? (
            <Alert variant="destructive" className="mb-4">
              <AlertDescription>
                {project.failedSyncTaskCount} task{project.failedSyncTaskCount > 1 ? "s" : ""} failed
                to sync with GitLab. Open a task from Tasks to see the error and retry.
              </AlertDescription>
            </Alert>
          ) : null}
          <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Created</dt>
              <dd className="text-foreground">{formatDateTime(project.createdAt)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Updated</dt>
              <dd className="text-foreground">{formatDateTime(project.updatedAt)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <div className="mt-8 grid gap-3 sm:grid-cols-2">
        <CollectionLink
          href={backlogsPath(project.id)}
          name="Backlogs"
          summary={countsError ? "Count unavailable" : `${backlogCount} backlogs`}
        />
        <CollectionLink
          href={tasksPath(project.id)}
          name="Tasks"
          summary={countsError ? "Count unavailable" : `${openTaskCount} open / ${taskCount} total`}
        />
        <CollectionLink
          href={gitlabConnectionPath(project.id)}
          name="GitLab connection"
          summary={gitlabSummary(gitlabConnection, linkedProjectCount)}
        />
      </div>

      <div className="mt-8">
        <VelocitySection velocity={velocity} error={velocityError} />
      </div>

      <div className="mt-8">
        <DeliveryMetricsSection
          metrics={metrics}
          flowMetrics={flowMetrics}
          from={metricsFrom}
          to={metricsTo}
          interval={metricsInterval}
          error={metricsError}
        />
      </div>

      <div className="mt-8">
        <FailedSyncJobSection jobs={failedSyncJobs} />
      </div>

      <div className="mt-8">
        <ProjectMemberSection
          projectId={project.id}
          members={members}
          currentUserId={currentUserId}
        />
      </div>

      <div className="mt-8">
        <ProjectInviteSection projectId={project.id} invites={invites} />
      </div>

      <div className="mt-8">
        <ApiTokenSection projectId={project.id} tokens={apiTokens} />
      </div>
    </>
  );
}
