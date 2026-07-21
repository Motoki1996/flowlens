import { redirect } from "next/navigation";

// The root simply forwards to the dashboard, which handles auth gating.
export default function Home() {
  redirect("/dashboard");
}
