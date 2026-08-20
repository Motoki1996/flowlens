"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { mergeRequestPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import type { MergeRequest, MergeRequestState } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
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
 * MERGE_REQUESTS_PER_PAGE is the page size the collection view asks the API
 * for. It is stated here rather than left to the API's own default because
 * the "N–M of T" range has to agree with the size actually requested, and
 * this component can't see the server's default.
 */
export const MERGE_REQUESTS_PER_PAGE = 30;

/**
 * MergeRequestListSection is the MergeRequest collection view of one project
 * (issue #112): state/author/sort/page are held in the URL, the same
 * hand-off-through-the-URL/server-refetch pattern AllTasksSection uses for
 * the cross-project Task collection, since this list is paged and filtered by
 * the API itself (`GET .../merge-requests`) rather than in the browser. A
 * merge request is never created, edited or deleted here — FlowLens only
 * mirrors what mrsync has imported (ADR-0011), so this screen has no actions
 * of its own beyond filtering and paging.
 *
 * Paging is "Load more"-shaped in the URL rather than in component state:
 * ?page= is a plain server round trip, so a deep page survives a refresh and
 * can be linked to, at the cost of replacing the list rather than appending
 * to it. Changing any filter resets back to page 1 — a page number is only
 * meaningful against the filter it was counted under.
 */
export function MergeRequestListSection({
  projectId,
  mergeRequests,
  state,
  author,
  sort,
  page = 1,
  perPage = MERGE_REQUESTS_PER_PAGE,
  nextPage = 0,
  totalCount = 0,
  error = false,
}: {
  projectId: string;
  mergeRequests: MergeRequest[];
  state?: MergeRequestState;
  author?: string;
  sort?: "updated";
  page?: number;
  perPage?: number;
  nextPage?: number;
  totalCount?: number;
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

  /** changeFilter is updateQuery plus the page reset every filter needs. */
  function changeFilter(next: Record<string, string | undefined>) {
    updateQuery({ ...next, page: undefined });
  }

  // Derived from perPage rather than this page's own length, which is short
  // on the last page and would shift the whole range.
  const rangeStart = mergeRequests.length === 0 ? 0 : (page - 1) * perPage + 1;
  const rangeEnd = (page - 1) * perPage + mergeRequests.length;

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
              onBlur={(e) => changeFilter({ author: e.target.value.trim() || undefined })}
              onKeyDown={(e) => {
                if (e.key === "Enter") changeFilter({ author: e.currentTarget.value.trim() || undefined });
              }}
            />
            <Select
              value={state ?? "all"}
              onValueChange={(value) => changeFilter({ state: value })}
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
              onValueChange={(value) => changeFilter({ sort: value })}
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
        {!error && (page > 1 || nextPage > 0) ? (
          <nav aria-label="Pagination" className="mt-4 flex items-center justify-between gap-3">
            <p className="text-muted-foreground text-xs">
              {rangeStart}–{rangeEnd} of {totalCount}
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
