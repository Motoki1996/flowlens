import { redirect } from "next/navigation";
import Link from "next/link";
import { getCurrentUser } from "@/lib/api";
import { SignupForm } from "@/components/SignupForm";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";

export default async function SignupPage() {
  // Already signed in -> go straight to the dashboard.
  const user = await getCurrentUser();
  if (user) redirect("/dashboard");

  return (
    <main className="flex min-h-screen items-center justify-center px-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">FlowLens</CardTitle>
          <CardDescription>
            Create an account to visualize your team&apos;s merge request flow.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <SignupForm />

          <p className="text-muted-foreground mt-4 text-center text-sm">
            Already have an account?{" "}
            <Link href="/login" className="text-foreground font-medium hover:underline">
              Sign in
            </Link>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
