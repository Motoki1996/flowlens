import { redirect } from "next/navigation";
import { getCurrentUser, API_PUBLIC_URL } from "@/lib/api";

const ERROR_MESSAGES: Record<string, string> = {
  invalid_state: "Login session expired. Please try again.",
  missing_code: "GitHub did not return an authorization code.",
  exchange_failed: "Could not complete sign-in with GitHub.",
  github_user_failed: "Could not read your GitHub profile.",
  user_persist_failed: "Could not save your account. Please try again.",
  session_failed: "Could not start a session. Please try again.",
};

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ error?: string }>;
}) {
  // Already signed in -> go straight to the dashboard.
  const user = await getCurrentUser();
  if (user) redirect("/dashboard");

  const { error } = await searchParams;
  const errorMessage = error ? (ERROR_MESSAGES[error] ?? "Sign-in failed.") : null;

  return (
    <main className="flex min-h-screen items-center justify-center px-6">
      <div className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-8 shadow-sm">
        <h1 className="text-xl font-semibold text-slate-900">FlowLens</h1>
        <p className="mt-2 text-sm text-slate-600">
          Sign in to visualize your team&apos;s pull request flow.
        </p>

        {errorMessage ? (
          <div
            role="alert"
            className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700"
          >
            {errorMessage}
          </div>
        ) : null}

        <a
          href={`${API_PUBLIC_URL}/auth/github`}
          className="mt-6 flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-800"
        >
          Sign in with GitHub
        </a>
      </div>
    </main>
  );
}
