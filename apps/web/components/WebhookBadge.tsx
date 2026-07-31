import type { LinkedGitlabProject } from "@/types";
import { Badge } from "@/components/ui/badge";

/** WebhookBadge states whether a linked GitLab project's webhook is registered.
 *  It is shared by the LinkedGitlabProject collection and single views so one
 *  status never reads two different ways. */
export function WebhookBadge({ status }: { status: LinkedGitlabProject["webhookStatus"] }) {
  switch (status) {
    case "registered":
      return <Badge variant="secondary">Webhook registered</Badge>;
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive text-destructive">
          Webhook failed
        </Badge>
      );
    default:
      return <Badge variant="outline">Webhook not registered</Badge>;
  }
}
