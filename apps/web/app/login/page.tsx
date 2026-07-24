import { redirect } from "next/navigation";
import Link from "next/link";
import { getCurrentUser } from "@/lib/api";
import { LoginForm } from "@/components/LoginForm";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";

export default async function LoginPage() {
  // Already signed in -> go straight to the dashboard.
  const user = await getCurrentUser();
  if (user) redirect("/dashboard");

  return (
    <main className="flex min-h-screen items-center justify-center px-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">FlowLens</CardTitle>
          <CardDescription>
            Sign in to visualize your team&apos;s merge request flow.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <LoginForm />

          <p className="text-muted-foreground mt-4 text-center text-sm">
            Don&apos;t have an account?{" "}
            <Link href="/signup" className="text-foreground font-medium hover:underline">
              Sign up
            </Link>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
