import { redirect } from "next/navigation";
import Link from "next/link";
import { getCurrentUser } from "@/lib/api";
import { SignupForm } from "@/components/SignupForm";

export default async function SignupPage() {
  // Already signed in -> go straight to the dashboard.
  const user = await getCurrentUser();
  if (user) redirect("/dashboard");

  return (
    <main className="flex min-h-screen items-center justify-center px-6">
      <div className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-8 shadow-sm">
        <h1 className="text-xl font-semibold text-slate-900">FlowLens</h1>
        <p className="mt-2 text-sm text-slate-600">
          Create an account to visualize your team&apos;s merge request flow.
        </p>

        <SignupForm />

        <p className="mt-4 text-center text-sm text-slate-600">
          Already have an account?{" "}
          <Link href="/login" className="font-medium text-slate-900 hover:underline">
            Sign in
          </Link>
        </p>
      </div>
    </main>
  );
}
