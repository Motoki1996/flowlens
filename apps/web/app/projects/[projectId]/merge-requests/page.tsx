import { notFound } from "next/navigation";
import { getMergeRequests, getProject } from "@/lib/api";
import {
  MergeRequestListSection,
  MERGE_REQUESTS_PER_PAGE,
} from "@/components/MergeRequestListSection";
import type { MergeRequestFilter } from "@/lib/api";

/**
 * The MergeRequest collection view of one project (issue #112): state,
 * author, sort and page live in the URL and are re-fetched server-side, the
 * same `GET .../merge-requests` query-param filtering the API itself does
 * (see MergeRequestListSection's doc comment).
 *
 * With no ?state=, the screen shows *open* merge requests sorted by recent
 * activity rather than every merge request the repository ever had. That is
 * what this view is for — reviewing what is in flight, and drilling into the
 * project's delivery metrics — and a repository synced for a year holds
 * thousands of merged ones behind it. "All states" is still one click away.
 *
 * Auth is guarded by the parent layout.tsx; this page doesn't render the
 * AppHeader so it has no need of its own user object.
 */
export const DEFAULT_STATE = "opened" as const;
export const DEFAULT_SORT = "updated" as const;

export default async function MergeRequestsPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
    state?: string;
    author?: string;
    sort?: string;
    page?: string;
  }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;

  // "all" is the explicit opt-out of the open-only default; it reaches the
  // API as no state filter at all.
  const rawState = resolvedSearchParams?.state ?? DEFAULT_STATE;
  const stateFilter = (rawState === "all" ? undefined : rawState) as MergeRequestFilter["state"];
  const authorFilter = resolvedSearchParams?.author;
  const sort = (resolvedSearchParams?.sort ?? DEFAULT_SORT) as MergeRequestFilter["sort"];
  const page = Number(resolvedSearchParams?.page) || 1;

  const project = await getProject(projectId);
  if (!project) notFound();

  let mergeRequests: Awaited<ReturnType<typeof getMergeRequests>>["mergeRequests"] = [];
  let nextPage = 0;
  let totalCount = 0;
  let error = false;
  try {
    const result = await getMergeRequests(projectId, {
      state: stateFilter,
      author: authorFilter,
      sort,
      page,
      perPage: MERGE_REQUESTS_PER_PAGE,
    });
    mergeRequests = result.mergeRequests;
    nextPage = result.nextPage;
    totalCount = result.totalCount;
  } catch {
    error = true;
  }

  return (
    <MergeRequestListSection
      projectId={project.id}
      mergeRequests={mergeRequests}
      state={stateFilter}
      author={authorFilter}
      sort={sort}
      page={page}
      perPage={MERGE_REQUESTS_PER_PAGE}
      nextPage={nextPage}
      totalCount={totalCount}
      error={error}
    />
  );
}
