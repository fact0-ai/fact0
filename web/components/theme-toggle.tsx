"use client"

import * as React from "react"
import { Moon, Sun } from "lucide-react"
import { useTheme } from "next-themes"

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const [mounted, setMounted] = React.useState(false)

  // Avoid hydration mismatch
  React.useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMounted(true)
  }, [])

  if (!mounted) return (
     <div className="size-9 rounded-xl bg-muted/50 border border-border/50 animate-pulse" />
  )

  return (
    <button
      onClick={() => setTheme(theme === "light" ? "dark" : "light")}
      className="relative size-9 rounded-xl bg-muted/50 border border-border/50 text-zinc-500 hover:text-indigo-600 dark:text-zinc-400 dark:hover:text-indigo-400 hover:border-indigo-500/30 transition-all flex items-center justify-center group"
      aria-label="Toggle theme"
    >
      <div className="relative size-4 flex items-center justify-center">
        <Sun className="absolute h-full w-full rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0 group-hover:rotate-12" />
        <Moon className="absolute h-full w-full rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100 group-hover:-rotate-12" />
      </div>
      <span className="sr-only">Toggle theme</span>
    </button>
  )
}
