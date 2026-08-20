import Link from "next/link";
import type { User } from "@/types";
import { allTasksPath } from "@/lib/routes";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { LogoutButton } from "./LogoutButton";
import { AppIcon } from "./AppIcon";

function initials(user: User) {
  const source = user.displayName || user.username;
  return source.slice(0, 2).toUpperCase();
}

/**
 * AppHeader is the top navigation bar shown on authenticated pages.
 *
 * `leading` is for controls that belong to the frame around the header rather
 * than to the app: the project screens pass the sidebar's toggle there. It
 * stays optional because most screens have no sidebar, and a SidebarTrigger
 * outside a SidebarProvider would throw.
 */
export function AppHeader({ user, leading }: { user: User; leading?: React.ReactNode }) {
  return (
    <header className="border-border bg-card border-b">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-3">
        <div className="flex items-center gap-6">
          {leading}
          <Link href="/dashboard" className="text-foreground flex items-center gap-2 text-lg font-semibold">
            <AppIcon className="size-6" />
            FlowLens
          </Link>
          <nav className="text-muted-foreground flex gap-4 text-sm">
            <Link href="/dashboard" className="hover:text-foreground">
              Dashboard
            </Link>
            <Link href="/projects" className="hover:text-foreground">
              Projects
            </Link>
            <Link href={allTasksPath()} className="hover:text-foreground">
              Tasks
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
