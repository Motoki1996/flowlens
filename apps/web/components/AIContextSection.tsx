"use client";

import { useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, TaskAIContext } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

type FieldKey = "acceptanceCriteria" | "aiContext";

const FIELDS: { key: FieldKey; label: string; placeholder: string }[] = [
  {
    key: "acceptanceCriteria",
    label: "Acceptance criteria",
    placeholder: "No acceptance criteria set.",
  },
  { key: "aiContext", label: "AI context", placeholder: "No AI context set." },
];

/**
 * AIContextField edits and saves a single task_ai_contexts field. Saving
 * always PUTs the full ai-context payload (the API upserts both fields
 * together), but only this field's value ever changes: the other one is
 * taken from the current, already-saved context. The allowed/forbidden
 * change scope lives on the task's Backlog instead (see BacklogEditForm) —
 * it describes a sub-area of the codebase, not one task.
 */
function AIContextField({
  taskId,
  fieldKey,
  label,
  placeholder,
  value,
  context,
  onSaved,
}: {
  taskId: string;
  fieldKey: FieldKey;
  label: string;
  placeholder: string;
  value: string;
  context: TaskAIContext;
  onSaved: (updated: TaskAIContext) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/ai-context`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          acceptanceCriteria: context.acceptanceCriteria,
          aiContext: context.aiContext,
          [fieldKey]: draft,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to save.");
        return;
      }
      onSaved((await res.json()) as TaskAIContext);
      setEditing(false);
    } finally {
      setPending(false);
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between gap-4">
        <h4 className="text-foreground text-sm font-medium">{label}</h4>
        {!editing ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setDraft(value);
              setError(null);
              setEditing(true);
            }}
          >
            Edit
          </Button>
        ) : null}
      </div>

      {editing ? (
        <form onSubmit={handleSubmit} className="mt-2 space-y-2" aria-label={`Edit ${label}`}>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <Textarea
            aria-label={label}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={4}
          />
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={pending}>
              {pending ? "Saving…" : "Save"}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setEditing(false)}
              disabled={pending}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <p className="text-foreground mt-1 text-sm whitespace-pre-wrap">
          {value || <span className="text-muted-foreground">{placeholder}</span>}
        </p>
      )}
    </div>
  );
}

/**
 * AIContextSection is the AI-facing information block on the task single
 * view: acceptance criteria and free-form AI context, each independently
 * editable and saved.
 */
export function AIContextSection({
  taskId,
  aiContext: initial,
}: {
  taskId: string;
  aiContext: TaskAIContext;
}) {
  const [context, setContext] = useState(initial);

  return (
    <div className="space-y-6">
      {FIELDS.map((field) => (
        <AIContextField
          key={field.key}
          taskId={taskId}
          fieldKey={field.key}
          label={field.label}
          placeholder={field.placeholder}
          value={context[field.key]}
          context={context}
          onSaved={setContext}
        />
      ))}
    </div>
  );
}
