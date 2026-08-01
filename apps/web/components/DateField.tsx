"use client";

import { useState } from "react";
import { CalendarIcon } from "lucide-react";
import { formatDateValue } from "@/lib/dates";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

/**
 * DateField is a date input rendered as a shadcn Calendar in a popover. The
 * trigger doubles as the labelled control, so the surrounding <label htmlFor>
 * names it the same way it would name an <input>. Shared by the creation form
 * in the Task collection and the edit form in the single view, so both pick
 * dates the same way.
 */
export function DateField({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value: Date | undefined;
  onChange: (date: Date | undefined) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div>
      <label htmlFor={id} className="text-foreground block text-sm font-medium">
        {label}
      </label>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            className="mt-1 w-full justify-between font-normal"
          >
            {value ? formatDateValue(value) : <span className="text-muted-foreground">Not set</span>}
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
