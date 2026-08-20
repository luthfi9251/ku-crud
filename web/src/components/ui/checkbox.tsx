import * as React from "react"

import { cn } from "@/lib/utils"

// Minimal checkbox primitive (Radix-free, matching the vendored switch/input
// pattern): a styled native input.
const Checkbox = React.forwardRef<HTMLInputElement, Omit<React.ComponentProps<"input">, "type">>(
  ({ className, ...props }, ref) => {
    return (
      <input
        type="checkbox"
        ref={ref}
        className={cn(
          "peer h-4 w-4 shrink-0 cursor-pointer appearance-none rounded border border-input shadow-sm transition-colors",
          "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
          "checked:border-blue-600 checked:bg-blue-600",
          "relative after:absolute after:left-1/2 after:top-1/2 after:h-2.5 after:w-1.5 after:-translate-x-1/2 after:-translate-y-1/2 after:rotate-45 after:border-b-2 after:border-r-2 after:border-white after:content-[''] after:opacity-0 checked:after:opacity-100",
          className
        )}
        {...props}
      />
    )
  }
)
Checkbox.displayName = "Checkbox"

export { Checkbox }
