"use client";

import { useState } from "react";
import Link from "next/link";
import type { Project } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { NewProjectForm } from "@/components/NewProjectForm";

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/**
 * ProjectsView is the presentational collection view for /projects. It takes
 * the already-fetched project list (or a fetch-error flag) as props so it
 * can be exercised with plain data, independent of the Server Component that
 * fetches it.
 */
export function ProjectsView({
  projects,
  error = false,
}: {
  projects: Project[];
  error?: boolean;
}) {
  const [showForm, setShowForm] = useState(false);

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-foreground text-2xl font-semibold">Projects</h1>
        {!error ? (
          <Button onClick={() => setShowForm((v) => !v)}>
            {showForm ? "Cancel" : "New project"}
          </Button>
        ) : null}
      </div>

      {showForm ? (
        <Card className="mt-6">
          <CardContent>
            <NewProjectForm onCancel={() => setShowForm(false)} />
          </CardContent>
        </Card>
      ) : null}

      {error ? (
        <Alert variant="destructive" className="mt-8">
          <AlertDescription>
            Failed to load projects. Try refreshing the page.
          </AlertDescription>
        </Alert>
      ) : projects.length === 0 ? (
        <Card className="mt-8 border-dashed">
          <CardHeader className="text-center">
            <CardTitle className="text-base font-medium">No projects yet</CardTitle>
            <CardDescription className="mx-auto max-w-md">
              Create a project to start tracking its delivery flow.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <ul className="mt-8 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((project) => (
            <li key={project.id}>
              <Link href={`/projects/${project.id}`}>
                <Card className="hover:border-ring h-full transition-colors">
                  <CardHeader>
                    <CardTitle className="text-base font-medium">{project.name}</CardTitle>
                    <CardDescription>
                      {project.description || "No description"}
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <p className="text-muted-foreground text-xs">
                      Created {formatDate(project.createdAt)}
                    </p>
                  </CardContent>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
