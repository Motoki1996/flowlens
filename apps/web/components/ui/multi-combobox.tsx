"use client"

import * as React from "react"
import { CheckIcon, ChevronsUpDownIcon, XIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

export type MultiComboboxOption = {
  value: string
  label: string
}

/**
 * MultiCombobox is Combobox's multi-select sibling: a Popover + Command
 * control that lets several options be toggled on/off, shown as removable
 * chips below the trigger. Typing a value that matches no existing option
 * offers to add it as a free-form entry, so callers aren't limited to a
 * fixed candidate list.
 */
export function MultiCombobox({
  id,
  options,
  value,
  onChange,
  placeholder = "Select…",
  searchPlaceholder = "Search…",
  emptyText = "No match found.",
  disabled = false,
  className,
  "aria-label": ariaLabel,
}: {
  id?: string
  options: MultiComboboxOption[]
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  disabled?: boolean
  className?: string
  "aria-label"?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState("")

  function toggle(v: string) {
    onChange(value.includes(v) ? value.filter((existing) => existing !== v) : [...value, v])
  }

  function remove(v: string) {
    onChange(value.filter((existing) => existing !== v))
  }

  const trimmedSearch = search.trim()
  const hasExactMatch = options.some(
    (option) => option.value.toLowerCase() === trimmedSearch.toLowerCase(),
  )

  return (
    <div className={cn("space-y-1.5", className)}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-label={ariaLabel}
            disabled={disabled}
            className="w-full justify-between font-normal"
          >
            <span className="text-muted-foreground truncate">
              {value.length > 0 ? `${value.length} selected` : placeholder}
            </span>
            <ChevronsUpDownIcon className="size-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          className="w-(--radix-popover-trigger-width) min-w-52 p-0"
          align="start"
        >
          <Command shouldFilter={false}>
            <CommandInput
              placeholder={searchPlaceholder}
              value={search}
              onValueChange={setSearch}
            />
            <CommandList>
              {options.filter((option) =>
                option.label.toLowerCase().includes(trimmedSearch.toLowerCase()),
              ).length === 0 && !trimmedSearch ? (
                <CommandEmpty>{emptyText}</CommandEmpty>
              ) : null}
              <CommandGroup>
                {options
                  .filter((option) => option.label.toLowerCase().includes(trimmedSearch.toLowerCase()))
                  .map((option) => (
                    <CommandItem
                      key={option.value}
                      value={option.label}
                      onSelect={() => toggle(option.value)}
                    >
                      <span className="truncate">{option.label}</span>
                      <CheckIcon
                        className={cn(
                          "ml-auto size-4",
                          value.includes(option.value) ? "opacity-100" : "opacity-0",
                        )}
                      />
                    </CommandItem>
                  ))}
                {trimmedSearch && !hasExactMatch ? (
                  <CommandItem
                    value={`__create__${trimmedSearch}`}
                    onSelect={() => {
                      toggle(trimmedSearch)
                      setSearch("")
                    }}
                  >
                    Add &quot;{trimmedSearch}&quot;
                  </CommandItem>
                ) : null}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {value.map((v) => {
            const label = options.find((option) => option.value === v)?.label ?? v
            return (
              <Badge key={v} variant="outline" className="pr-1">
                {label}
                <button
                  type="button"
                  aria-label={`Remove ${label}`}
                  onClick={() => remove(v)}
                  disabled={disabled}
                  className="hover:text-foreground ml-0.5 rounded-full"
                >
                  <XIcon className="size-3" />
                </button>
              </Badge>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}
