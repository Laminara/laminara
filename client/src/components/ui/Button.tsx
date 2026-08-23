import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "@/lib/format";

type Variant = "primary" | "secondary" | "ghost";
type Size = "md" | "sm";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  icon?: ReactNode;
}

const sizes: Record<Size, string> = {
  md: "px-5 py-3",
  sm: "px-3 py-2",
};

const variants: Record<Variant, string> = {
  primary: "bg-primary text-primary-ink hover:bg-primary-strong active:brightness-95",
  secondary: "border border-border bg-surface-2 text-text hover:bg-surface-3",
  ghost: "text-dim hover:bg-surface-2 hover:text-text",
};

export function Button({ variant = "primary", size = "md", icon, children, className, ...rest }: ButtonProps) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-md text-sm font-semibold tracking-wide transition-colors disabled:opacity-50",
        sizes[size],
        variants[variant],
        className,
      )}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
}
