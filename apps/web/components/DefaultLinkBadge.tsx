import type { LinkedGitlabProject } from "@/types";
import { Badge } from "@/components/ui/badge";

/** DefaultLinkBadge marks the linked GitLab project a new task is pushed to
 *  when it names none itself. Like WebhookBadge it is shared by the
 *  LinkedGitlabProject collection and single views, so the one designation
 *  never reads two different ways. A non-default link renders nothing: only
 *  one link per connection carries this, so the absence is the other state. */
export function DefaultLinkBadge({ isDefault }: { isDefault: LinkedGitlabProject["isDefault"] }) {
  if (!isDefault) return null;
  return <Badge variant="secondary">Default</Badge>;
}
