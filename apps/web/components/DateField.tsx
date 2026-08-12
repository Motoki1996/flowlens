"use client";

import { useState } from "react";
import { CalendarIcon } from "lucide-react";
import { formatDateValue } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

/**
 * DateField is a date input rendered as a shadcn Calendar in a popover. The
 * trigger doubles as the labelled control, so the surrounding <label htmlFor>
 * names it the same way it would name an <input>. Shared by the creation form
 * in the Task collection and the edit form in the single view, so both pick
 * dates the same way.
 *
 * `hideLabel` drops the visible <label> and moves it onto the trigger as an
 * aria-label instead, for the inline filter row in the Project view's delivery
 * metrics, where the surrounding card header already says what the pair of
 * dates means and a stacked label would not fit. `placeholder` names the empty
 * state, which reads differently for a filter ("From") than for a form field
 * ("Not set"). Both keep those call sites on the same calendar as the forms.
 */
export function DateField({
  id,
  label,
  value,
  onChange,
  placeholder = "Not set",
  hideLabel = false,
  className,
}: {
  id: string;
  label: string;
  value: Date | undefined;
  onChange: (date: Date | undefined) => void;
  placeholder?: string;
  hideLabel?: boolean;
  className?: string;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div className={hideLabel ? "contents" : undefined}>
      {!hideLabel && (
        <label htmlFor={id} className="text-foreground block text-sm font-medium">
          {label}
        </label>
      )}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            aria-label={hideLabel ? label : undefined}
            className={cn("w-full justify-between font-normal", !hideLabel && "mt-1", className)}
          >
            {value ? formatDateValue(value) : <span className="text-muted-foreground">{placeholder}</span>}
            <CalendarIcon className="size-4 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <Calendar
            mode="single"
            selected={value}
            defaultMonth={value}
            captionLayout="dropdown"
            autoFocus
            onSelect={(date) => {
              onChange(date);
              setOpen(false);
            }}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}
