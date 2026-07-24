import Link from "next/link";
import type { User } from "@/types";
import { LogoutButton } from "./LogoutButton";

/** AppHeader is the top navigation bar shown on authenticated pages. */
export function AppHeader({ user }: { user: User }) {
  return (
    <header className="border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-3">
        <div className="flex items-center gap-6">
          <Link href="/dashboard" className="text-lg font-semibold text-slate-900">
            FlowLens
          </Link>
          <nav className="flex gap-4 text-sm text-slate-600">
            <Link href="/dashboard" className="hover:text-slate-900">
              Dashboard
            </Link>
            <Link href="/settings" className="hover:text-slate-900">
              Settings
            </Link>
          </nav>
        </div>
        <div className="flex items-center gap-3">
          <span className="hidden text-sm text-slate-600 sm:inline">
            {user.displayName || user.username}
          </span>
          <LogoutButton />
        </div>
      </div>
    </header>
  );
}
