import type React from "react";

/**
 * CreateFormRegion separates an inline "New …" form from the list it sits
 * above. A form and a list of rows are both stacks of bordered boxes, so
 * without a rule between them it is genuinely unclear where the form ends —
 * the reader has to work it out from the buttons.
 */
export function CreateFormRegion({ children }: { children: React.ReactNode }) {
  return <div className="border-border mb-6 border-b pb-6">{children}</div>;
}
