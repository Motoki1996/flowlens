import Link from "next/link";
import { getCurrentUser, getInvitePreview } from "@/lib/api";
import { AcceptInviteButton } from "@/components/AcceptInviteButton";
import { SignupForm } from "@/components/SignupForm";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";

/**
 * The invite acceptance screen (issue #211). Reachable without a session —
 * middleware.ts excludes /invites — because the person it is for may have no
 * account at all, which is the whole reason invites exist: an instance with
 * ALLOW_SIGNUP=false has no other way to onboard anyone.
 *
 * Auth flows are docs/ui-design.md's deliberate task-oriented exception
 * (rule 7), which is where this belongs: it is not a view of an object, it
 * is the act of joining.
 */
export default async function InvitePage({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  const [preview, user] = await Promise.all([getInvitePreview(token), getCurrentUser()]);

  if (!preview) {
    return (
      <main className="flex min-h-screen items-center justify-center px-6">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle className="text-xl">Invite unavailable</CardTitle>
            <CardDescription>
              This invite link is invalid, has expired, or has already been used. Ask whoever
              invited you for a new one.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-center text-sm">
              <Link href="/login" className="text-foreground font-medium hover:underline">
                Go to sign in
              </Link>
            </p>
          </CardContent>
        </Card>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center px-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Join {preview.projectName}</CardTitle>
          <CardDescription>
            You have been invited to <strong>{preview.projectName}</strong> as a {preview.role}.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {user ? (
            <>
              <p className="text-muted-foreground mb-4 text-sm">
                Signed in as {user.username}.
              </p>
              <AcceptInviteButton token={token} projectId={preview.projectId} />
            </>
          ) : (
            <>
              <SignupForm inviteToken={token} submitLabel="Create account & join" />
              <p className="text-muted-foreground mt-4 text-center text-sm">
                Already have an account?{" "}
                <Link href="/login" className="text-foreground font-medium hover:underline">
                  Sign in
                </Link>{" "}
                and open this link again.
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </main>
  );
}
