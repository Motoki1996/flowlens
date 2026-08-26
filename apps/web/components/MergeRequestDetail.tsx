import Link from "next/link";
import { taskPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import type { MergeRequest, Task } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { MergeRequestStateBadge } from "@/components/MergeRequestStateBadge";
import { PipelineStatusBadge } from "@/components/PipelineStatusBadge";
import { TruncatedName } from "@/components/TruncatedName";

/**
 * MergeRequestDetail is the single view for one merge request (issue #112),
 * in the order docs/ui-design.md rule 6 fixes: identity (title, number,
 * state) -> attributes (branches, size, timestamps, review/pipeline status)
 * -> related objects (its linked task, GitLab itself). It is read-only —
 * FlowLens never writes a merge request back to GitLab (ADR-0011), so unlike
 * TaskDetail there is no edit form, no close/reopen, no delete: every field
 * here is exactly what mrsync last imported.
 */
export function MergeRequestDetail({
  mergeRequest: mr,
  projectId,
  task = null,
}: {
  mergeRequest: MergeRequest;
  projectId: string;
  // The task mr.taskId points to, or null when the merge request references
  // no task (or references one outside this project — see the page's own
  // doc comment) or when it doesn't reference a task at all.
  task?: Task | null;
}) {
  return (
    <>
      <Card>
        <CardHeader>
          {/* min-w-0 at every level: CardHeader is a grid, and a grid item's
              automatic minimum size is its content's, so a nowrap title would
              size the track and run the card off the screen instead of being
              cut. */}
          <div className="flex min-w-0 items-start justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <TruncatedName
                  as="h1"
                  text={`!${mr.number} ${mr.title}`}
                  className="text-foreground text-xl leading-none font-semibold"
                />
                {mr.isDraft ? (
                  <Badge variant="outline" className="shrink-0">
                    Draft
                  </Badge>
                ) : null}
                <span className="shrink-0">
                  <MergeRequestStateBadge state={mr.state} />
                </span>
              </div>
              <CardDescription className="mt-1.5">
                {mr.authorGitlabUsername || "Unknown author"} wants to merge{" "}
                <code className="text-xs">{mr.headBranch}</code> into{" "}
                <code className="text-xs">{mr.baseBranch}</code>
              </CardDescription>
            </div>
            <div className="shrink-0">
              <a
                href={mr.htmlUrl}
                target="_blank"
                rel="noreferrer"
                className="text-primary text-sm underline underline-offset-2"
              >
                View on GitLab
              </a>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Pipeline</dt>
              <dd className="text-foreground">
                <PipelineStatusBadge status={mr.pipelineStatus} />
                {!mr.pipelineStatus ? "No pipeline recorded" : null}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">First reviewed</dt>
              <dd className="text-foreground">
                {mr.firstReviewedAt ? formatDate(mr.firstReviewedAt) : "Not yet reviewed"}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Size</dt>
              <dd className="text-foreground">
                +{mr.additions} -{mr.deletions} ({mr.changedFiles} file
                {mr.changedFiles === 1 ? "" : "s"})
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Opened</dt>
              <dd className="text-foreground">
                {mr.gitlabCreatedAt ? formatDate(mr.gitlabCreatedAt) : "Unknown"}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Merged</dt>
              <dd className="text-foreground">{mr.mergedAt ? formatDate(mr.mergedAt) : "Not merged"}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Closed</dt>
              <dd className="text-foreground">{mr.closedAt ? formatDate(mr.closedAt) : "Not closed"}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card className="mt-8">
        <CardHeader>
          <CardTitle className="text-base font-medium">Task</CardTitle>
          <CardDescription>
            The task this merge request&rsquo;s description or branch name referenced.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {task ? (
            <Link
              href={taskPath(projectId, task.id)}
              className="text-primary text-sm underline underline-offset-2"
            >
              {task.title}
            </Link>
          ) : (
            <p className="text-muted-foreground text-sm">No task linked to this merge request.</p>
          )}
        </CardContent>
      </Card>
    </>
  );
}
