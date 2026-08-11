"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { mergeRequestPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import type { MergeRequest, MergeRequestState } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { MergeRequestStateBadge } from "@/components/MergeRequestStateBadge";
import { PipelineStatusBadge } from "@/components/PipelineStatusBadge";

const STATES: MergeRequestState[] = ["opened", "merged", "closed", "locked"];

/**
 * MergeRequestListSection is the MergeRequest collection view of one project
 * (issue #112): state/author/sort are held in the URL, the same
 * hand-off-through-the-URL/server-refetch pattern AllTasksSection uses for
 * the cross-project Task collection, since this list is filtered by the API
 * itself (`GET .../merge-requests`) rather than in the browser. A merge
 * request is never created, edited or deleted here — FlowLens only mirrors
 * what mrsync has imported (ADR-0011), so this screen has no actions of its
 * own beyond filtering.
 */
export function MergeRequestListSection({
  projectId,
  mergeRequests,
  state,
  author,
  sort,
  error = false,
}: {
  projectId: string;
  mergeRequests: MergeRequest[];
  state?: MergeRequestState;
  author?: string;
  sort?: "updated";
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Merge requests</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              aria-label="Author"
              placeholder="Filter by author…"
              defaultValue={author ?? ""}
              className="h-8 w-40"
              onBlur={(e) => updateQuery({ author: e.target.value.trim() || undefined })}
              onKeyDown={(e) => {
                if (e.key === "Enter") updateQuery({ author: e.currentTarget.value.trim() || undefined });
              }}
            />
            <Select
              value={state ?? "all"}
              onValueChange={(value) => updateQuery({ state: value === "all" ? undefined : value })}
            >
              <SelectTrigger size="sm" aria-label="State" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All states</SelectItem>
                {STATES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s[0].toUpperCase() + s.slice(1)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={sort ?? "created"}
              onValueChange={(value) => updateQuery({ sort: value === "created" ? undefined : value })}
            >
              <SelectTrigger size="sm" aria-label="Sort" className="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="created">Newest created</SelectItem>
                <SelectItem value="updated">Recently updated</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {error ? (
          <p className="text-destructive text-sm">
            Failed to load merge requests. Try refreshing the page.
          </p>
        ) : mergeRequests.length === 0 ? (
          <p className="text-muted-foreground text-sm">No merge requests match the current filters.</p>
        ) : (
          <ul className="space-y-2">
            {mergeRequests.map((mr) => (
              <li key={mr.id}>
                <Link
                  href={mergeRequestPath(projectId, mr.id)}
                  className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="text-foreground truncate">
                      !{mr.number} {mr.title}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      {mr.authorGitlabUsername || "Unknown author"}
                      {mr.gitlabCreatedAt ? ` · opened ${formatDate(mr.gitlabCreatedAt)}` : ""}
                    </span>
                  </span>
                  <span className="flex shrink-0 items-center gap-2 text-xs">
                    {mr.isDraft ? <span className="text-muted-foreground">Draft</span> : null}
                    <PipelineStatusBadge status={mr.pipelineStatus} />
                    <MergeRequestStateBadge state={mr.state} />
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
