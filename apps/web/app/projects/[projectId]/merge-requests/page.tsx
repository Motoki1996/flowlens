import { notFound } from "next/navigation";
import { getMergeRequests, getProject } from "@/lib/api";
import { MergeRequestListSection } from "@/components/MergeRequestListSection";
import type { MergeRequestFilter } from "@/lib/api";

/**
 * The MergeRequest collection view of one project (issue #112): state,
 * author and sort live in the URL and are re-fetched server-side, the same
 * `GET .../merge-requests` query-param filtering the API itself does (see
 * MergeRequestListSection's doc comment).
 *
 * Auth is guarded by the parent layout.tsx; this page doesn't render the
 * AppHeader so it has no need of its own user object.
 */
export default async function MergeRequestsPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
    state?: string;
    author?: string;
    sort?: string;
  }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const stateFilter = resolvedSearchParams?.state as MergeRequestFilter["state"];
  const authorFilter = resolvedSearchParams?.author;
  const sort = resolvedSearchParams?.sort as MergeRequestFilter["sort"];

  const project = await getProject(projectId);
  if (!project) notFound();

  let mergeRequests: Awaited<ReturnType<typeof getMergeRequests>> = [];
  let error = false;
  try {
    mergeRequests = await getMergeRequests(projectId, {
      state: stateFilter,
      author: authorFilter,
      sort,
    });
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
      error={error}
    />
  );
}
