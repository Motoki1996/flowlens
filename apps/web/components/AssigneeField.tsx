"use client";

import { useMemo } from "react";
import type { ProjectMember } from "@/types";
import { Combobox } from "@/components/ui/combobox";

/** The Combobox's sentinel for "no FlowLens assignee" — a project member's id
 *  is a UUID, so it can't collide with a real option's value. Mirrors
 *  TaskEditForm's own UNASSIGNED for the GitLab-mirrored assignee field. */
export const UNASSIGNED_MEMBER = "unassigned";

/** The minimal shape of an object's *current* assignee, kept as an option
 *  even if it fell off `members` (e.g. the member was removed from the
 *  project) — the same rule TaskEditForm's assigneeSelectOptions follows for
 *  the GitLab-mirrored field, so opening a form never silently drops the
 *  existing selection. */
export interface CurrentAssignee {
  userId: string;
  username: string;
  displayName: string;
}

export function assigneeSelectOptions(
  members: ProjectMember[] | null,
  current: CurrentAssignee | null,
) {
  if (!members) return [];
  const known = members.map((m) => ({
    value: m.userId,
    label: m.displayName ? `${m.displayName} (@${m.username})` : m.username,
  }));
  if (current && !members.some((m) => m.userId === current.userId)) {
    known.unshift({
      value: current.userId,
      label: current.displayName
        ? `${current.displayName} (@${current.username})`
        : current.username || "Unknown member",
    });
  }
  return [{ value: UNASSIGNED_MEMBER, label: "Unassigned" }, ...known];
}

/**
 * AssigneeField picks the FlowLens project member who owns a Task, Backlog
 * or Epic (`assigneeUserId`) — a separate axis from a task's GitLab-mirrored
 * assignee (see TaskEditForm). Shared by all three objects' edit forms since
 * the field means the same thing and is populated from the same
 * `GET /api/v1/projects/{projectID}/members` list on each.
 *
 * Renders nothing when `members` is null (the listing fetch failed): there is
 * no option set to offer, and the caller's own current assignee still shows
 * up read-only elsewhere on the object's screen.
 */
export function AssigneeField({
  id,
  members,
  current,
  value,
  onChange,
  className = "mt-1",
}: {
  id: string;
  members: ProjectMember[] | null;
  current: CurrentAssignee | null;
  value: string;
  onChange: (value: string) => void;
  className?: string;
}) {
  const options = useMemo(() => assigneeSelectOptions(members, current), [members, current]);
  if (!members) return null;
  return (
    <Combobox
      id={id}
      options={options}
      value={value}
      onChange={onChange}
      searchPlaceholder="Search members…"
      emptyText="No member found."
      className={className}
    />
  );
}
