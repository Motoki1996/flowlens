"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, Project } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** EditProjectForm is the inline edit form shown in place of the identity block. */
function EditProjectForm({
  project,
  onSaved,
  onCancel,
}: {
  project: Project;
  onSaved: (project: Project) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Project name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${project.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, description }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to update project.");
        return;
      }
      onSaved((await res.json()) as Project);
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" aria-label="Edit project">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div>
        <label htmlFor="edit-project-name" className="text-foreground block text-sm font-medium">
          Name
        </label>
        <Input
          id="edit-project-name"
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <label htmlFor="edit-project-description" className="text-foreground block text-sm font-medium">
          Description
        </label>
        <Textarea
          id="edit-project-description"
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** DeleteProjectButton interposes an inline confirmation before deleting. */
function DeleteProjectButton({ project }: { project: Project }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${project.id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete project.");
        setPending(false);
        return;
      }
      router.push("/projects");
      router.refresh();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex items-center gap-2">
        {error ? <span className="text-destructive text-sm">{error}</span> : null}
        <span className="text-foreground text-sm">Delete this project?</span>
        <Button variant="destructive" size="sm" onClick={handleDelete} disabled={pending}>
          {pending ? "Deleting…" : "Confirm delete"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
          Cancel
        </Button>
      </div>
    );
  }

  return (
    <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
      Delete
    </Button>
  );
}

/**
 * ProjectDetail is the single view for one project. Attribute order is
 * fixed per docs/ui-design.md: identity -> attributes -> related
 * collections. Edit and delete actions live here rather than on a separate
 * "manage" screen.
 */
export function ProjectDetail({ project: initial }: { project: Project }) {
  const [project, setProject] = useState(initial);
  const [editing, setEditing] = useState(false);

  return (
    <>
      <Card>
        <CardHeader>
          {editing ? (
            <EditProjectForm
              project={project}
              onSaved={(updated) => {
                setProject(updated);
                setEditing(false);
              }}
              onCancel={() => setEditing(false)}
            />
          ) : (
            <>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h1 className="text-foreground text-xl leading-none font-semibold">
                    {project.name}
                  </h1>
                  <CardDescription className="mt-1.5">
                    {project.description || "No description"}
                  </CardDescription>
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                    Edit
                  </Button>
                  <DeleteProjectButton project={project} />
                </div>
              </div>
            </>
          )}
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Created</dt>
              <dd className="text-foreground">{formatDateTime(project.createdAt)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Updated</dt>
              <dd className="text-foreground">{formatDateTime(project.updatedAt)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <div className="mt-8 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base font-medium">Backlog</CardTitle>
            <CardDescription>Coming soon.</CardDescription>
          </CardHeader>
        </Card>
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base font-medium">Tasks</CardTitle>
            <CardDescription>Coming soon.</CardDescription>
          </CardHeader>
        </Card>
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base font-medium">GitLab connection</CardTitle>
            <CardDescription>Coming soon.</CardDescription>
          </CardHeader>
        </Card>
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base font-medium">Sync history</CardTitle>
            <CardDescription>Coming soon.</CardDescription>
          </CardHeader>
        </Card>
      </div>
    </>
  );
}
