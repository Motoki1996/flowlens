import Link from "next/link";
import type { User } from "@/types";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { LogoutButton } from "./LogoutButton";

function initials(user: User) {
  const source = user.displayName || user.username;
  return source.slice(0, 2).toUpperCase();
}

/** AppHeader is the top navigation bar shown on authenticated pages. */
export function AppHeader({ user }: { user: User }) {
  return (
    <header className="border-border bg-card border-b">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-3">
        <div className="flex items-center gap-6">
          <Link href="/dashboard" className="text-foreground text-lg font-semibold">
            FlowLens
          </Link>
          <nav className="text-muted-foreground flex gap-4 text-sm">
            <Link href="/dashboard" className="hover:text-foreground">
              Dashboard
            </Link>
            <Link href="/settings" className="hover:text-foreground">
              Settings
            </Link>
          </nav>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-muted-foreground hidden text-sm sm:inline">
            {user.displayName || user.username}
          </span>
          <Avatar className="size-8">
            <AvatarFallback>{initials(user)}</AvatarFallback>
          </Avatar>
          <LogoutButton />
        </div>
      </div>
    </header>
  );
}
