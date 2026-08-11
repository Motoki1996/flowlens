import { notFound } from "next/navigation";
import { getMergeRequest, getProject, getTask } from "@/lib/api";
import { mergeRequestsPath } from "@/lib/routes";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { MergeRequestDetail } from "@/components/MergeRequestDetail";

/**
 * The MergeRequest single view (issue #112). Read-only, per
 * components/MergeRequestDetail's own doc comment — this page's only job is
 * gathering the merge request, its project and (if linked) its task.
 *
 * Auth is guarded by the parent layout.tsx; this page doesn't render the
 * AppHeader so it has no need of its own user object.
 */
export default async function MergeRequestPage({
  params,
}: {
  params: Promise<{ projectId: string; mrId: string }>;
}) {
  const { projectId, mrId } = await params;
  const [mergeRequest, project] = await Promise.all([getMergeRequest(mrId), getProject(projectId)]);
  if (!mergeRequest || !project) notFound();

  const task = mergeRequest.taskId ? await getTask(mergeRequest.taskId) : null;

  return (
    <>
      <Breadcrumbs
        items={[
          { label: "Merge requests", href: mergeRequestsPath(project.id) },
          { label: `!${mergeRequest.number}` },
        ]}
      />
      <MergeRequestDetail mergeRequest={mergeRequest} projectId={project.id} task={task} />
    </>
  );
}
